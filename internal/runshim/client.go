package runshim

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
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
)

// Client talks to one shim over its socket. It is safe for concurrent use. Replay opens a second
// connection, so a slow consumer of the stream never delays a signal or a stdin write.
type Client struct {
	socket  string
	version int
	conn    net.Conn

	wmu  sync.Mutex
	enc  *json.Encoder
	mu   sync.Mutex
	next int64
	wait map[int64]chan message
	err  error // set once the connection is dead

	streams []*Stream
}

// Dial connects to the shim at socket and checks the protocol version.
func Dial(socket string) (*Client, error) { return DialVersion(socket, ProtocolVersion) }

// DialVersion is Dial announcing version instead of ProtocolVersion; tests use it to provoke a
// mismatch.
func DialVersion(socket string, version int) (*Client, error) {
	conn, err := dialHello(socket, version)
	if err != nil {
		return nil, err
	}
	c := &Client{socket: socket, version: version, conn: conn, enc: json.NewEncoder(conn), wait: map[int64]chan message{}}
	go c.readLoop(bufio.NewReaderSize(conn, 64<<10))
	return c, nil
}

// dialHello opens a connection and completes the hello exchange.
func dialHello(socket string, version int) (net.Conn, error) {
	conn, err := net.DialTimeout("unix", socket, callTimeout)
	if err != nil {
		return nil, fmt.Errorf("connect to shim: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(callTimeout))
	if err := json.NewEncoder(conn).Encode(request{ID: 1, Op: opHello, Version: version}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("shim hello: %w", err)
	}
	// The decoder may read ahead, which is safe: after hello the shim sends only answers to
	// requests, and none has been made yet.
	var resp message
	dec := json.NewDecoder(conn)
	if err := dec.Decode(&resp); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("shim hello: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	if !resp.OK {
		_ = conn.Close()
		if resp.Code == codeVersion {
			return nil, fmt.Errorf("%w: %s", ErrVersionMismatch, resp.Error)
		}
		return nil, fmt.Errorf("shim hello refused: %s", resp.Error)
	}
	return conn, nil
}

func (c *Client) readLoop(r *bufio.Reader) {
	dec := json.NewDecoder(r)
	for {
		var m message
		if err := dec.Decode(&m); err != nil {
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
	_, err := c.call(request{Op: opWriteStdin, Data: b}, 0)
	return err
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
	conn, err := dialHello(c.socket, c.version)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(callTimeout))
	if err := json.NewEncoder(conn).Encode(request{ID: 2, Op: opReplay, From: from}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("shim replay: %w", err)
	}
	dec := json.NewDecoder(bufio.NewReaderSize(conn, 64<<10))
	var resp message
	if err := dec.Decode(&resp); err != nil {
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
			if err := dec.Decode(&m); err != nil {
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
