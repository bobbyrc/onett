package runshim

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShimDropsAnOversizedRequest: a request with no end must not make the shim buffer forever.
func TestShimDropsAnOversizedRequest(t *testing.T) {
	sh := startInProc(t, "sleep 30", nil)
	conn, err := net.Dial("unix", sh.spec.SocketPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	br := bufio.NewReader(conn)
	_, err = conn.Write([]byte(`{"id":1,"op":"hello","version":1}` + "\n"))
	require.NoError(t, err)
	line, err := br.ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, line, `"ok":true`)

	go func() {
		_, _ = conn.Write([]byte(`{"id":2,"op":"write_stdin","data":"`))
		_, _ = conn.Write(bytes.Repeat([]byte("A"), 17<<20))
	}()
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	_, err = br.ReadByte()
	require.Error(t, err)
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatal("the shim kept reading a request with no end instead of dropping the connection")
	}
	_, err = sh.client.Status()
	require.NoError(t, err, "the shim still serves other clients")
}

// TestClientDropsAnOversizedMessage: a message from the shim with no end must not make the client
// buffer forever.
func TestClientDropsAnOversizedMessage(t *testing.T) {
	sock, err := SocketPath(t.TempDir(), "big-1")
	require.NoError(t, err)
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		br := bufio.NewReader(c)
		_, _ = br.ReadString('\n')
		_, _ = c.Write([]byte(`{"type":"response","id":1,"ok":true,"version":1}` + "\n"))
		_, _ = br.ReadString('\n')
		_, _ = c.Write([]byte(`{"type":"response","id":2,"ok":true,"error":"`))
		_, _ = c.Write(bytes.Repeat([]byte("a"), 62<<20))
		<-done
		_ = c.Close()
	}()

	c, err := Dial(sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Status()
	require.ErrorIs(t, err, ErrClosed, "the client must drop a message with no end, not wait for more of it")
}

// TestStderrLineIsCutShort: a stderr line longer than the status budget is cut, not kept whole.
func TestStderrLineIsCutShort(t *testing.T) {
	sh := startInProc(t, "head -c 100000 /dev/zero | tr '\\0' 'x' >&2; echo >&2; sleep 30", nil)
	var st Status
	require.Eventually(t, func() bool {
		var err error
		st, err = sh.client.Status()
		return err == nil && len(st.StderrTail) >= 1
	}, 10*time.Second, 20*time.Millisecond)
	assert.Equal(t, 64<<10, len(st.StderrTail[0]))
}

// TestWriteStdinLargerThanOneRequestArrivesWhole: the client splits a large write, so the
// shim's limit on one request never cuts real input.
func TestWriteStdinLargerThanOneRequestArrivesWhole(t *testing.T) {
	sh := startInProc(t, "wc -c", func(s *Spec) { s.KeepStdinOpen = true })
	require.NoError(t, sh.client.WriteStdin(bytes.Repeat([]byte("a"), 20<<20)))
	require.NoError(t, sh.client.CloseStdin())
	s, err := sh.client.Replay(0)
	require.NoError(t, err)
	got := lines(collect(t, s))
	require.Len(t, got, 1)
	assert.Equal(t, "20971520", strings.TrimSpace(got[0]))
}
