package runshim

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bobbyrc/onett/internal/sysproc"
)

const (
	// exitGrace is how long a process group left behind by an exited agent gets after SIGTERM,
	// and again after SIGKILL, before its pipes are cut. Same as the harness's procExitGrace.
	exitGrace = 2 * time.Second
	// statusInterval throttles status.json rewrites caused by output alone.
	statusInterval = time.Second
	// stderrBuffer is how many live stderr lines a connected client may lag behind.
	stderrBuffer = 256
)

// shim is one running shim: the agent, its log and its socket.
type shim struct {
	spec Spec
	cmd  *exec.Cmd
	logf *os.File
	ln   net.Listener

	stdin       io.WriteCloser // nil unless KeepStdinOpen
	stdinMu     sync.Mutex     // serialises writers only; never held by closeStdin
	stdinClosed atomic.Bool
	stdinOnce   sync.Once

	readers  sync.WaitGroup // stdout and stderr readers
	stdoutF  *os.File
	stderrF  *os.File
	finished chan struct{} // closed when the agent has exited and its output is drained

	mu      sync.Mutex
	st      Status
	changed chan struct{} // closed and replaced whenever the log, the stderr stream or the exit changes
	subs    map[chan string]struct{}
	conns   map[net.Conn]struct{}

	ackSeen bool // a client has acked at least once; an empty log still needs an ack

	statusMu    sync.Mutex
	lastWritten time.Time

	quit     chan struct{}
	quitOnce sync.Once
}

// Run is the shim: it starts the agent, serves the socket and returns once the shim should
// exit, which is when the agent has exited and a client acked the final offset, when the grace
// period after the agent's exit passes, or when ctx is cancelled (the agent's group is killed
// then). The socket is removed on the way out; stream.log and status.json stay.
func Run(ctx context.Context, spec Spec) error {
	switch {
	case len(spec.Command) == 0:
		return errors.New("run shim: empty command")
	case spec.RunID == "" || spec.RunDir == "" || spec.SocketPath == "":
		return errors.New("run shim: run id, run dir and socket path are required")
	}
	if spec.Grace <= 0 {
		spec.Grace = DefaultGrace
	}
	if err := os.MkdirAll(spec.RunDir, 0o700); err != nil {
		return fmt.Errorf("run shim: %w", err)
	}
	s := &shim{
		spec:     spec,
		finished: make(chan struct{}),
		changed:  make(chan struct{}),
		subs:     map[chan string]struct{}{},
		conns:    map[net.Conn]struct{}{},
		quit:     make(chan struct{}),
	}
	s.st = Status{RunID: spec.RunID, Version: ProtocolVersion, ShimPID: os.Getpid(), Socket: spec.SocketPath}

	if err := s.start(); err != nil {
		s.st.StartError = err.Error()
		s.st.Exited, s.st.ExitCode = true, -1
		now := time.Now()
		s.st.ExitedAt = &now
		s.writeStatus(true)
		return err
	}
	if err := s.listen(); err != nil {
		_ = syscall.Kill(-s.st.AgentPGID, syscall.SIGKILL)
		return err
	}
	go s.accept()
	go s.wait()

	select {
	case <-s.quit:
	case <-ctx.Done():
		_ = sysproc.KillGroup(s.st.AgentPGID, syscall.SIGKILL)
		select {
		case <-s.finished:
		case <-time.After(5 * time.Second):
		}
	}
	s.shutdown()
	return nil
}

// start opens the log, starts the agent in its own process group and begins draining its output.
func (s *shim) start() error {
	logf, err := os.OpenFile(StreamPath(s.spec.RunDir), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open stream log: %w", err)
	}
	s.logf = logf

	cmd := exec.Command(s.spec.Command[0], s.spec.Command[1:]...)
	cmd.Dir = s.spec.Dir
	cmd.Env = s.spec.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if s.spec.KeepStdinOpen {
		w, err := cmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("stdin pipe: %w", err)
		}
		s.stdin = w
	} else if s.spec.Stdin != "" {
		cmd.Stdin = strings.NewReader(s.spec.Stdin)
	}
	if s.stdin == nil {
		s.stdinClosed.Store(true)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		_, _ = outR.Close(), outW.Close()
		return fmt.Errorf("stderr pipe: %w", err)
	}
	cmd.Stdout, cmd.Stderr = outW, errW
	if err := cmd.Start(); err != nil {
		for _, f := range []*os.File{outR, outW, errR, errW} {
			_ = f.Close()
		}
		return fmt.Errorf("start %s: %w", s.spec.Command[0], err)
	}
	// The agent holds its own copies of the write ends now.
	_, _ = outW.Close(), errW.Close()
	s.cmd, s.stdoutF, s.stderrF = cmd, outR, errR

	now := time.Now()
	s.st.AgentPID, s.st.AgentPGID = cmd.Process.Pid, cmd.Process.Pid
	s.st.StartedAt, s.st.LastOutputAt = now, now
	s.writeStatus(true)

	s.readers.Add(2)
	go s.readStdout(outR)
	go s.readStderr(errR)

	if s.stdin != nil && s.spec.Stdin != "" {
		// Hold the writer lock while the first input goes out so a later write_stdin cannot
		// overtake it, and write from a goroutine so input larger than the pipe buffer does not
		// delay the socket coming up.
		s.stdinMu.Lock()
		go func() {
			defer s.stdinMu.Unlock()
			if _, err := s.stdin.Write([]byte(s.spec.Stdin)); err != nil {
				s.closeStdin()
			}
		}()
	}
	return nil
}

// listen binds the socket. A leftover socket file from a shim that is gone is replaced; one that
// still answers belongs to a live shim and is an error.
func (s *shim) listen() error {
	path := s.spec.SocketPath
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		_ = c.Close()
		return fmt.Errorf("run shim: socket %s is in use", path)
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("run shim: listen: %w", err)
	}
	_ = os.Chmod(path, 0o600)
	s.ln = ln
	s.mu.Lock()
	s.st.Socket = path
	s.mu.Unlock()
	s.writeStatus(true)
	return nil
}

func (s *shim) readStdout(r *os.File) {
	defer s.readers.Done()
	defer r.Close()
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := readLine(br, MaxLine)
		if text := strings.TrimRight(line, "\r\n"); text != "" {
			s.appendLine(text)
		}
		if err != nil {
			return
		}
	}
}

// appendLine adds one line to the log. If the disk write fails the line is lost to replay but
// the pipe is still drained, so the agent never blocks on us.
func (s *shim) appendLine(text string) {
	buf := make([]byte, 0, len(text)+1)
	buf = append(append(buf, text...), '\n')
	n, _ := s.logf.Write(buf)
	s.mu.Lock()
	// Count what reached the disk, so offsets match the file even after a failed write.
	s.st.LogSize += int64(n)
	s.st.LastOutputAt = time.Now()
	s.broadcastLocked()
	s.mu.Unlock()
	s.writeStatus(false)
}

func (s *shim) readStderr(r *os.File) {
	defer s.readers.Done()
	defer r.Close()
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := readLine(br, MaxLine)
		if text := strings.TrimRight(line, "\r\n"); text != "" {
			s.mu.Lock()
			s.st.StderrTail = append(s.st.StderrTail, text)
			if len(s.st.StderrTail) > StderrTailLines {
				s.st.StderrTail = s.st.StderrTail[len(s.st.StderrTail)-StderrTailLines:]
			}
			for ch := range s.subs {
				select {
				case ch <- text:
				default: // a lagging client loses live stderr; the tail in status keeps the last lines
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// readLine returns the next line without reading past max bytes. Bytes beyond max are
// discarded up to the newline. At EOF it returns any unterminated tail with io.EOF.
func readLine(br *bufio.Reader, max int) (string, error) {
	var buf []byte
	truncated := false
	for {
		chunk, err := br.ReadSlice('\n')
		if !truncated {
			if len(buf)+len(chunk) > max {
				buf = append(buf, chunk[:max-len(buf)]...)
				truncated = true
			} else {
				buf = append(buf, chunk...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return string(buf), err
	}
}

// wait reaps the agent, then its process group, and records the exit.
func (s *shim) wait() {
	_ = s.cmd.Wait()
	code := -1
	if s.cmd.ProcessState != nil {
		code = s.cmd.ProcessState.ExitCode()
	}
	s.closeStdin()
	s.reapGroup()

	now := time.Now()
	s.mu.Lock()
	s.st.Exited, s.st.ExitCode, s.st.ExitedAt = true, code, &now
	s.broadcastLocked()
	s.mu.Unlock()
	s.writeStatus(true)
	close(s.finished)

	time.AfterFunc(s.spec.Grace, s.requestQuit)
	s.maybeQuit()
}

// reapGroup runs once the agent has exited. A background child it started still holds the
// output pipes, so the readers would never see EOF: the group gets SIGTERM, then SIGKILL, and
// as a last resort the read ends are given an expired deadline. The harness does the same for a
// direct child.
func (s *shim) reapGroup() {
	pgid := s.st.AgentPGID
	readersDone := make(chan struct{})
	go func() {
		s.readers.Wait()
		close(readersDone)
	}()
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		_ = sysproc.KillGroup(pgid, sig)
		select {
		case <-readersDone:
			return
		case <-time.After(exitGrace):
		}
	}
	_ = s.stdoutF.SetReadDeadline(time.Now())
	_ = s.stderrF.SetReadDeadline(time.Now())
	<-readersDone
}

func (s *shim) closeStdin() {
	s.stdinClosed.Store(true)
	s.stdinOnce.Do(func() {
		if s.stdin != nil {
			_ = s.stdin.Close()
		}
	})
}

var errStdinClosed = errors.New("agent stdin is closed")

func (s *shim) writeStdin(b []byte) error {
	s.stdinMu.Lock()
	defer s.stdinMu.Unlock()
	if s.stdinClosed.Load() {
		return errStdinClosed
	}
	if _, err := s.stdin.Write(b); err != nil {
		s.closeStdin()
		return fmt.Errorf("%w: %v", errStdinClosed, err)
	}
	return nil
}

// broadcastLocked wakes everything waiting for a change. s.mu must be held.
func (s *shim) broadcastLocked() {
	close(s.changed)
	s.changed = make(chan struct{})
}

// snapshot returns a copy of the status.
func (s *shim) snapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.st
	st.StderrTail = append([]string(nil), st.StderrTail...)
	return st
}

// writeStatus replaces status.json. Unless force is set, a write that follows another by less
// than statusInterval is skipped, so a chatty agent does not rewrite the file for every line.
func (s *shim) writeStatus(force bool) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if !force && time.Since(s.lastWritten) < statusInterval {
		return
	}
	s.lastWritten = time.Now()
	b, err := json.MarshalIndent(s.snapshot(), "", "  ")
	if err != nil {
		return
	}
	path := StatusPath(s.spec.RunDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

// maybeQuit ends the shim once the agent has exited and a client acked the final offset.
func (s *shim) maybeQuit() {
	s.mu.Lock()
	ok := s.st.Exited && s.ackSeen && s.st.Acked >= s.st.LogSize
	s.mu.Unlock()
	if ok {
		s.requestQuit()
	}
}

func (s *shim) requestQuit() { s.quitOnce.Do(func() { close(s.quit) }) }

// shutdown closes the socket and every connection and removes the socket file.
func (s *shim) shutdown() {
	// Closing a Go unix listener unlinks its socket file, which would make "the socket is gone"
	// true while this shim still has files to write, and let a replacement shim bind the same
	// path just before the remove below deletes the replacement's socket.
	if ul, ok := s.ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	_ = s.ln.Close()
	s.mu.Lock()
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	_ = s.logf.Close()
	s.writeStatus(true)
	// Last, so a missing socket means the shim is done with its files.
	_ = os.Remove(s.spec.SocketPath)
}

func (s *shim) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns[c] = struct{}{}
		s.mu.Unlock()
		go s.serveConn(c)
	}
}

// connWriter serialises the messages written to one connection.
type connWriter struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (w *connWriter) send(m message) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.enc.Encode(m)
}

func (s *shim) serveConn(c net.Conn) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		_ = c.Close()
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
	}()
	w := &connWriter{enc: json.NewEncoder(c)}
	dec := json.NewDecoder(bufio.NewReaderSize(c, 64<<10))

	var req request
	if err := dec.Decode(&req); err != nil {
		return
	}
	if req.Op != opHello {
		_ = w.send(message{Type: msgResponse, ID: req.ID, Error: "the first request must be hello"})
		return
	}
	if req.Version != ProtocolVersion {
		_ = w.send(message{Type: msgResponse, ID: req.ID, Code: codeVersion, Version: ProtocolVersion,
			Error: fmt.Sprintf("protocol version mismatch: client speaks %d, shim speaks %d", req.Version, ProtocolVersion)})
		return
	}
	if w.send(message{Type: msgResponse, ID: req.ID, OK: true, Version: ProtocolVersion}) != nil {
		return
	}

	var replaying atomic.Bool
	for {
		var req request
		if err := dec.Decode(&req); err != nil {
			return
		}
		// Each request runs on its own goroutine, so a write_stdin stuck on a full pipe does not
		// stop a close_stdin or signal behind it. The client waits for each write's response
		// before sending the next, which keeps writes in order.
		go func() {
			resp := s.handle(ctx, w, req, &replaying)
			if resp != nil {
				resp.Type, resp.ID = msgResponse, req.ID
				_ = w.send(*resp)
			}
			if req.Op == opAck {
				s.maybeQuit()
			}
		}()
	}
}

// handle runs one request and returns its response, or nil when handle sent it itself.
func (s *shim) handle(ctx context.Context, w *connWriter, req request, replaying *atomic.Bool) *message {
	fail := func(err error) *message { return &message{Error: err.Error()} }
	switch req.Op {
	case opStatus:
		st := s.snapshot()
		return &message{OK: true, Status: &st}
	case opAck:
		s.mu.Lock()
		defer s.mu.Unlock()
		if req.Offset < 0 || req.Offset > s.st.LogSize {
			return fail(fmt.Errorf("ack %d is outside the log (size %d)", req.Offset, s.st.LogSize))
		}
		s.ackSeen = true
		if req.Offset > s.st.Acked {
			s.st.Acked = req.Offset
		}
		return &message{OK: true}
	case opWriteStdin:
		if err := s.writeStdin(req.Data); err != nil {
			m := fail(err)
			if errors.Is(err, errStdinClosed) {
				m.Code = codeStdinClosed
			}
			return m
		}
		return &message{OK: true}
	case opCloseStdin:
		s.closeStdin()
		return &message{OK: true}
	case opSignal:
		return s.signal(req.Signal)
	case opReplay:
		if err := s.checkOffset(req.From); err != nil {
			return fail(err)
		}
		if !replaying.CompareAndSwap(false, true) {
			return fail(errors.New("this connection is already replaying"))
		}
		// Send the response before the first line so a client can rely on the order.
		_ = w.send(message{Type: msgResponse, ID: req.ID, OK: true})
		go s.follow(ctx, w, req.From)
		return nil
	default:
		return fail(fmt.Errorf("unknown op %q", req.Op))
	}
}

func (s *shim) signal(name string) *message {
	var sig syscall.Signal
	switch strings.TrimPrefix(strings.ToUpper(name), "SIG") {
	case "TERM":
		sig = syscall.SIGTERM
	case "KILL":
		sig = syscall.SIGKILL
	default:
		return &message{Error: fmt.Sprintf("unsupported signal %q (use SIGTERM or SIGKILL)", name)}
	}
	s.mu.Lock()
	exited := s.st.Exited
	s.mu.Unlock()
	if !exited { // the group ID may be reused once the agent is gone
		if err := sysproc.KillGroup(s.st.AgentPGID, sig); err != nil {
			return &message{Error: err.Error()}
		}
	}
	return &message{OK: true}
}

// checkOffset accepts 0, the log size, and any offset that follows a newline.
func (s *shim) checkOffset(off int64) error {
	s.mu.Lock()
	size := s.st.LogSize
	s.mu.Unlock()
	if off < 0 || off > size {
		return fmt.Errorf("offset %d is outside the log (size %d)", off, size)
	}
	if off == 0 {
		return nil
	}
	f, err := os.Open(StreamPath(s.spec.RunDir))
	if err != nil {
		return err
	}
	defer f.Close()
	var b [1]byte
	if _, err := f.ReadAt(b[:], off-1); err != nil || b[0] != '\n' {
		return fmt.Errorf("offset %d is not at the start of a line", off)
	}
	return nil
}

// follow sends every logged line from offset from, then keeps sending new ones, and ends with
// an eof message once the agent has exited and the log is fully sent.
func (s *shim) follow(ctx context.Context, w *connWriter, from int64) {
	f, err := os.Open(StreamPath(s.spec.RunDir))
	if err != nil {
		return
	}
	defer f.Close()
	sub := make(chan string, stderrBuffer)
	s.mu.Lock()
	s.subs[sub] = struct{}{}
	// A replay from the start begins with the stderr lines already written, so a client that
	// connects just after a fast agent has run still sees them. Stderr is not logged, so this is
	// at most the last StderrTailLines lines.
	var backlog []string
	if from == 0 {
		backlog = append(backlog, s.st.StderrTail...)
	}
	s.mu.Unlock()
	for _, text := range backlog {
		if w.send(message{Type: MsgStderr, Text: text}) != nil {
			return
		}
	}
	defer func() {
		s.mu.Lock()
		delete(s.subs, sub)
		s.mu.Unlock()
	}()

	off := from
	for {
		// Live stderr first, so it is not held back behind a busy stdout.
	drain:
		for {
			select {
			case text := <-sub:
				if w.send(message{Type: MsgStderr, Text: text}) != nil {
					return
				}
			default:
				break drain
			}
		}
		s.mu.Lock()
		size, done, code, changed := s.st.LogSize, s.st.Exited, s.st.ExitCode, s.changed
		s.mu.Unlock()

		if off < size {
			r := bufio.NewReaderSize(io.NewSectionReader(f, off, size-off), 64<<10)
			for off < size {
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				next := off + int64(len(line))
				if w.send(message{Type: MsgLine, Offset: off, Next: next, Text: strings.TrimSuffix(line, "\n")}) != nil {
					return
				}
				off = next
			}
			continue
		}
		if done {
			_ = w.send(message{Type: MsgEOF, Offset: size, ExitCode: code})
			return
		}
		select {
		case <-changed:
		case text := <-sub:
			if w.send(message{Type: MsgStderr, Text: text}) != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
