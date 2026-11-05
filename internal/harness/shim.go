package harness

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
	if p.KeepStdinOpen {
		proc.stdin = shimStdin{client}
	} else {
		proc.stdinClosed.Store(true)
	}

	proc.readers.Add(1)
	go func() {
		defer proc.readers.Done()
		proc.readShim(stream, client, p.ParseLine)
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
func (p *Proc) readShim(stream *runshim.Stream, client *runshim.Client, parse func(string) []Event) {
	exitCode := -1
	var exitErr error
	defer func() {
		p.exitCode, p.waitErr = exitCode, exitErr
		close(p.exited)
		p.CloseStdin()
	}()
	for m := range stream.C {
		switch m.Type {
		case runshim.MsgLine:
			if text := strings.TrimRight(m.Text, "\r\n"); text != "" && parse != nil {
				for _, ev := range parse(text) {
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
