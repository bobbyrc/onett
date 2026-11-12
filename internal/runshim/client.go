package runshim

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"syscall"
	"time"
)

// callTimeout bounds the requests that must answer quickly. write_stdin has no bound: like a
// write to a pipe it waits for the agent to read, and ends when the connection or stdin closes.
const callTimeout = 10 * time.Second

var (
	// ErrVersionMismatch is returned by Dial when the shim speaks another protocol version.
	ErrVersionMismatch = errors.New("runshim: protocol version mismatch")
	// ErrStdinClosed is returned by WriteStdin once the agent's stdin is closed.
	ErrStdinClosed = errors.New("runshim: agent stdin is closed")
	// ErrClosed is returned by calls on a closed or broken connection.
	ErrClosed = errors.New("runshim: connection closed")
	// ErrWrongPeer is returned when the process answering on a shim's socket is not that shim.
	ErrWrongPeer = errors.New("runshim: the socket is answered by another process")
)

// Client talks to one shim over its socket. It is safe for concurrent use. Replay opens a second
// connection, so a slow consumer of the stream never delays a signal or a stdin write.
type Client struct {
	socket  string
	version int
	pid     int // the process that answered, as the kernel reports it; 0 when the platform cannot tell
	conn    net.Conn

	wmu  sync.Mutex
	enc  *json.Encoder
	mu   sync.Mutex
	next int64
	wait map[int64]chan message
	err  error // set once the connection is dead

	streams []*Stream
}

// Dial connects to the shim at socket and checks the protocol version. It takes whoever
// answers there for the shim; code that knows which process the shim is uses Start or
// DialStatus.
func Dial(socket string) (*Client, error) { return dial(socket, ProtocolVersion, 0) }

// DialVersion is Dial announcing version instead of ProtocolVersion; tests use it to provoke a
// mismatch.
func DialVersion(socket string, version int) (*Client, error) { return dial(socket, version, 0) }

// DialStatus connects to the shim that wrote st, the content of a status.json, and checks that
// the process answering on st.Socket is st.ShimPID; if not it returns ErrWrongPeer. The check
// is against the kernel's record of who is listening, so another process that took the socket
// path cannot pass it. It proves no more than st itself: a process that can rewrite
// status.json can name itself.
func DialStatus(st Status) (*Client, error) {
	if st.Socket == "" {
		return nil, errors.New("runshim: the status names no socket")
	}
	if st.ShimPID <= 0 {
		// Every shim so far has written its process ID. Should one not have, it is reached the
		// old way instead of refused: refusing would leave its agent running with nobody watching.
		slog.Warn("run shim status names no process ID; not checking who answers on its socket",
			"run", st.RunID, "socket", st.Socket)
		return Dial(st.Socket)
	}
	return dial(st.Socket, ProtocolVersion, st.ShimPID)
}

// dial is Dial for a shim that must be process pid (0: whoever answers).
func dial(socket string, version, pid int) (*Client, error) {
	conn, br, peer, err := dialHello(socket, version, pid)
	if err != nil {
		return nil, err
	}
	c := &Client{socket: socket, version: version, pid: peer, conn: conn, enc: json.NewEncoder(conn), wait: map[int64]chan message{}}
	go c.readLoop(br)
	return c, nil
}

// dialHello opens a connection and completes the hello exchange. With wantPID set, the process
// at the other end must be that one, and nothing is sent to it before that is known. It
// returns the reader to go on with and the peer's process ID (0 when the platform cannot tell).
func dialHello(socket string, version, wantPID int) (net.Conn, *bufio.Reader, int, error) {
	conn, err := net.DialTimeout("unix", socket, callTimeout)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("connect to shim: %w", err)
	}
	fail := func(err error) (net.Conn, *bufio.Reader, int, error) {
		_ = conn.Close()
		return nil, nil, 0, err
	}
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return fail(errors.New("connect to shim: not a unix socket"))
	}
	pid, err := peerPID(uc)
	if err != nil {
		return fail(fmt.Errorf("connect to shim: peer process: %w", err))
	}
	if peerPIDSupported && wantPID != 0 && pid != wantPID {
		return fail(fmt.Errorf("%w: %s is held by process %d, not shim %d", ErrWrongPeer, socket, pid, wantPID))
	}
	_ = conn.SetDeadline(time.Now().Add(callTimeout))
	if err := json.NewEncoder(conn).Encode(request{ID: 1, Op: opHello, Version: version}); err != nil {
		return fail(fmt.Errorf("shim hello: %w", err))
	}
	br := bufio.NewReaderSize(conn, 64<<10)
	var resp message
	if err := readJSON(br, maxMessage, &resp); err != nil {
		return fail(fmt.Errorf("shim hello: %w", err))
	}
	_ = conn.SetDeadline(time.Time{})
	if !resp.OK {
		if resp.Code == codeVersion {
			return fail(fmt.Errorf("%w: %s", ErrVersionMismatch, resp.Error))
		}
		return fail(fmt.Errorf("shim hello refused: %s", resp.Error))
	}
	return conn, br, pid, nil
}

func (c *Client) readLoop(r *bufio.Reader) {
	for {
		var m message
		if err := readJSON(r, maxMessage, &m); err != nil {
			c.fail(err)
			return
		}
		c.mu.Lock()
		ch := c.wait[m.ID]
		delete(c.wait, m.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	}
}

// fail marks the connection dead and wakes every pending call.
func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = fmt.Errorf("%w: %v", ErrClosed, err)
	}
	pending := c.wait
	c.wait = map[int64]chan message{}
	c.mu.Unlock()
	for _, ch := range pending {
		close(ch)
	}
}

// call sends req and waits for its response; timeout 0 waits as long as the connection lives.
func (c *Client) call(req request, timeout time.Duration) (message, error) {
	ch := make(chan message, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return message{}, err
	}
	c.next++
	req.ID = c.next + 1 // ID 1 was the hello
	c.wait[req.ID] = ch
	c.mu.Unlock()

	c.wmu.Lock()
	err := c.enc.Encode(req)
	c.wmu.Unlock()
	if err != nil {
		c.fail(err)
		return message{}, c.deadErr()
	}
	var timer <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timer = t.C
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return message{}, c.deadErr()
		}
		if !m.OK {
			if m.Code == codeStdinClosed {
				return m, ErrStdinClosed
			}
			return m, fmt.Errorf("shim %s: %s", req.Op, m.Error)
		}
		return m, nil
	case <-timer:
		c.mu.Lock()
		delete(c.wait, req.ID)
		c.mu.Unlock()
		return message{}, fmt.Errorf("shim %s: no answer after %s", req.Op, timeout)
	}
}

func (c *Client) deadErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return ErrClosed
}

// WriteStdin writes b to the agent's stdin. It returns ErrStdinClosed once stdin is closed.
func (c *Client) WriteStdin(b []byte) error {
	// In pieces, so that no request comes near the shim's limit on one request (maxRequest).
	for {
		n := min(len(b), stdinChunk)
		if _, err := c.call(request{Op: opWriteStdin, Data: b[:n]}, 0); err != nil {
			return err
		}
		if b = b[n:]; len(b) == 0 {
			return nil
		}
	}
}

// CloseStdin closes the agent's stdin.
func (c *Client) CloseStdin() error {
	_, err := c.call(request{Op: opCloseStdin}, callTimeout)
	return err
}

// Signal sends sig, which must be SIGTERM or SIGKILL, to the agent's process group.
func (c *Client) Signal(sig syscall.Signal) error {
	name := ""
	switch sig {
	case syscall.SIGTERM:
		name = "SIGTERM"
	case syscall.SIGKILL:
		name = "SIGKILL"
	default:
		return fmt.Errorf("runshim: unsupported signal %v", sig)
	}
	_, err := c.call(request{Op: opSignal, Signal: name}, callTimeout)
	return err
}

// Status returns the shim's current status.
func (c *Client) Status() (Status, error) {
	m, err := c.call(request{Op: opStatus}, callTimeout)
	if err != nil {
		return Status{}, err
	}
	if m.Status == nil {
		return Status{}, errors.New("shim status: empty answer")
	}
	return *m.Status, nil
}

// Ack tells the shim the client has processed the log up to offset. Once the agent has exited and
// the final offset is acked, the shim exits.
func (c *Client) Ack(offset int64) error {
	_, err := c.call(request{Op: opAck, Offset: offset}, callTimeout)
	return err
}

// Close drops the connections. The agent and the shim keep running.
func (c *Client) Close() error {
	c.fail(errors.New("closed by client"))
	c.mu.Lock()
	streams := c.streams
	c.streams = nil
	c.mu.Unlock()
	for _, s := range streams {
		s.Close()
	}
	return c.conn.Close()
}

// StreamMsg is one message of a replay: a stdout line (Type MsgLine, with Offset and Next), a live
// stderr line (MsgStderr) or the end (MsgEOF, with the final Offset and the agent's ExitCode).
type StreamMsg struct {
	Type     string
	Offset   int64
	Next     int64
	Text     string
	ExitCode int
}

// Stream is a running replay. C delivers messages in order and is closed after the MsgEOF
// message, or without one when the connection to the shim drops.
type Stream struct {
	C    <-chan StreamMsg
	conn net.Conn
	done chan struct{}
	once sync.Once
}

// Close ends the replay.
func (s *Stream) Close() {
	s.once.Do(func() { close(s.done) })
	_ = s.conn.Close()
}

// Replay streams the stdout lines from byte offset from (0 or an offset a previous stream
// reported as Next), then follows new ones live. It uses its own connection.
func (c *Client) Replay(from int64) (*Stream, error) {
	conn, br, _, err := dialHello(c.socket, c.version, c.pid)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(callTimeout))
	if err := json.NewEncoder(conn).Encode(request{ID: 2, Op: opReplay, From: from}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("shim replay: %w", err)
	}
	var resp message
	if err := readJSON(br, maxMessage, &resp); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("shim replay: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	if !resp.OK {
		_ = conn.Close()
		return nil, fmt.Errorf("shim replay: %s", resp.Error)
	}
	out := make(chan StreamMsg, 64)
	s := &Stream{C: out, conn: conn, done: make(chan struct{})}
	c.mu.Lock()
	c.streams = append(c.streams, s)
	c.mu.Unlock()
	go func() {
		defer close(out)
		for {
			var m message
			if err := readJSON(br, maxMessage, &m); err != nil {
				return
			}
			select {
			case out <- StreamMsg{Type: m.Type, Offset: m.Offset, Next: m.Next, Text: m.Text, ExitCode: m.ExitCode}:
			case <-s.done:
				return
			}
			if m.Type == MsgEOF {
				return
			}
		}
	}()
	return s, nil
}
