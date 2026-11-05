package harness

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/runshim"
	"github.com/bobbyrc/onett/internal/sysproc"
)

// ShimOptions turns on limits.run_shim for a harness: its processes run behind a per-run shim
// (see package runshim) that keeps the agent alive if onett exits.
type ShimOptions struct {
	// DataDir is onett's data directory; the shim's files go under <DataDir>/runs/<run id>/.
	DataDir string
	// Launch is how to start the shim, normally {onett, "run-shim"} from os.Executable.
	Launch runshim.Launch
	// Grace overrides how long a shim outlives its agent without a final ack (0 = 24h).
	Grace time.Duration
}

// shimStdin adapts the shim client to the writer Proc keeps for the agent's stdin.
type shimStdin struct{ c *runshim.Client }

func (w shimStdin) Write(b []byte) (int, error) {
	if err := w.c.WriteStdin(b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (w shimStdin) Close() error { return w.c.CloseStdin() }

// startShimProcess is StartProcess for ProcSpec.Shim: the same Proc, with the agent owned by a
// shim process and its output read back over the shim's socket instead of from pipes.
func startShimProcess(ctx context.Context, p ProcSpec) (*Proc, error) {
	runID := p.RunID
	if runID == "" {
		var b [8]byte
		_, _ = rand.Read(b[:])
		runID = "adhoc-" + hex.EncodeToString(b[:])
	}
	sock, err := runshim.SocketPath(p.Shim.DataDir, runID)
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", p.Command[0], err)
	}
	env := sysproc.AgentEnv(p.InheritEnv, p.Env)
	client, err := runshim.Start(ctx, p.Shim.Launch, runshim.Spec{
		RunID:         runID,
		RunDir:        runshim.RunDir(p.Shim.DataDir, runID),
		SocketPath:    sock,
		Command:       p.Command,
		Dir:           p.Dir,
		Env:           env,
		Stdin:         p.Stdin,
		KeepStdinOpen: p.KeepStdinOpen,
		Grace:         p.Shim.Grace,
	})
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", p.Command[0], err)
	}
	return attachShim(ctx, p, client, 0)
}

// ReattachProcess is StartProcess for a process that an earlier onett started behind a shim and
// that may still be running: it dials the run's shim and follows its output, delivering events
// only for lines from stream offset from on (see Reattacher). p must be the ProcSpec the process
// was started with, with p.Shim set. Nothing is started; if the shim cannot be reached the error
// says so.
func ReattachProcess(ctx context.Context, p ProcSpec, from int64) (*Proc, error) {
	if p.Shim == nil || p.RunID == "" {
		return nil, errors.New("reattach: needs a run ID and shim options")
	}
	runDir := runshim.RunDir(p.Shim.DataDir, p.RunID)
	sock := ""
	if st, err := runshim.ReadStatus(runDir); err == nil {
		sock = st.Socket
	}
	if sock == "" {
		var err error
		if sock, err = runshim.SocketPath(p.Shim.DataDir, p.RunID); err != nil {
			return nil, fmt.Errorf("reattach %s: %w", p.RunID, err)
		}
	}
	client, err := runshim.Dial(sock)
	if err != nil {
		return nil, fmt.Errorf("reattach %s: %w", p.RunID, err)
	}
	proc, err := attachShim(ctx, p, client, from)
	if err != nil {
		return nil, fmt.Errorf("reattach %s: %w", p.RunID, err)
	}
	return proc, nil
}

// attachShim turns a connected shim client into a Proc. The whole stdout log is replayed so the
// parser rebuilds its state, but events for lines before offset from are dropped.
func attachShim(ctx context.Context, p ProcSpec, client *runshim.Client, from int64) (*Proc, error) {
	stream, err := client.Replay(0)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("start %s: %w", p.Command[0], err)
	}

	proc := &Proc{
		events: make(chan Event, procEventBuffer),
		onExit: p.OnExit,
		exited: make(chan struct{}),
		done:   make(chan struct{}),
	}
	proc.signalGroup = func(sig syscall.Signal) { _ = client.Signal(sig) }
	proc.detach = func() {
		stream.Close()
		_ = client.Close()
	}
	if p.KeepStdinOpen {
		proc.stdin = shimStdin{client}
	} else {
		proc.stdinClosed.Store(true)
	}

	proc.readers.Add(1)
	go func() {
		defer proc.readers.Done()
		proc.readShim(stream, client, p.ParseLine, from)
	}()
	go func() {
		proc.readers.Wait()
		<-proc.exited
		close(proc.events)
		close(proc.done)
		_ = client.Close()
	}()
	if ctx != nil {
		go func() {
			select {
			case <-ctx.Done():
				proc.Cancel()
			case <-proc.done:
			}
		}()
	}
	return proc, nil
}

// readShim feeds the shim's stream into Events until the agent's exit message, then records the
// exit and acks the whole log so the shim can stop. A stream that ends without that message
// means the shim is gone; the process is then reported as failed with exit code -1.
func (p *Proc) readShim(stream *runshim.Stream, client *runshim.Client, parse func(string) []Event, from int64) {
	exitCode := -1
	var exitErr error
	defer func() {
		p.exitCode, p.waitErr = exitCode, exitErr
		close(p.exited)
		if !p.detached.Load() {
			// Not for a detached agent: it keeps running, and its stdin must stay open.
			p.CloseStdin()
		}
	}()
	for m := range stream.C {
		switch m.Type {
		case runshim.MsgLine:
			if text := strings.TrimRight(m.Text, "\r\n"); text != "" && parse != nil {
				// Lines before from were delivered by an earlier process; they are parsed
				// only to rebuild the parser's state.
				evs := parse(text)
				if m.Offset < from {
					continue
				}
				for i, ev := range evs {
					if i == len(evs)-1 {
						ev.Offset = m.Next
					}
					p.emit(ev)
				}
			}
		case runshim.MsgStderr:
			p.tailMu.Lock()
			p.tail = append(p.tail, m.Text)
			if len(p.tail) > procStderrTail {
				p.tail = p.tail[len(p.tail)-procStderrTail:]
			}
			p.tailMu.Unlock()
			p.emit(Event{Kind: domain.LogStderr, Text: m.Text})
		case runshim.MsgEOF:
			exitCode = m.ExitCode
			if exitCode != 0 {
				exitErr = fmt.Errorf("exit status %d", exitCode)
			}
			_ = client.Ack(m.Offset)
			return
		}
	}
	exitErr = fmt.Errorf("lost the run shim before the process exited")
}

// ReplayProcessLog parses a finished shim's stream.log without a shim to talk to: it is what is
// left when the daemon died and the shim went with it. The whole log goes through the parser, so
// its state is rebuilt, but only events for lines at or after from are returned, with Event.Offset
// set on the last event of each line as in a live session. A last line without its newline was
// cut short by the crash and is ignored. A missing log is not an error: there is nothing to replay.
func ReplayProcessLog(p ProcSpec, from int64) ([]Event, error) {
	if p.Shim == nil || p.RunID == "" || p.ParseLine == nil {
		return nil, errors.New("replay log: needs a run ID, shim options and a parser")
	}
	f, err := os.Open(runshim.StreamPath(runshim.RunDir(p.Shim.DataDir, p.RunID)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("replay log: %w", err)
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 64<<10)
	var out []Event
	var off int64
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return out, fmt.Errorf("replay log: %w", err)
			}
			return out, nil
		}
		next := off + int64(len(line))
		if text := strings.TrimRight(line, "\r\n"); text != "" {
			evs := p.ParseLine(text)
			if off >= from {
				for i, ev := range evs {
					if i == len(evs)-1 {
						ev.Offset = next
					}
					out = append(out, ev)
				}
			}
		}
		off = next
	}
}
