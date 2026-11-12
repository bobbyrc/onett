package runshim

import (
	"bufio"
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStartRefusesAnImpostorOnTheSocket: another process that already holds the socket path and
// answers hello correctly must not be taken for the shim Start launched.
func TestStartRefusesAnImpostorOnTheSocket(t *testing.T) {
	if !peerPIDSupported {
		t.Skip("no peer check on this platform")
	}
	dataDir := t.TempDir()
	sock, err := SocketPath(dataDir, "impostor-1")
	require.NoError(t, err)
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	done := make(chan struct{})
	t.Cleanup(func() { _ = ln.Close() })
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _ = bufio.NewReader(c).ReadString('\n')
				_, _ = c.Write([]byte(`{"type":"response","id":1,"ok":true,"version":1}` + "\n"))
				<-done
				_ = c.Close()
			}()
		}
	}()

	// A stand-in shim that never opens the socket and stops on SIGTERM. Bounded, so it ends by
	// itself if this test process dies.
	script := `trap 'exit 0' TERM
i=0
while [ $i -lt 600 ]; do sleep 0.05; i=$((i+1)); done`
	l := Launch{Command: []string{"sh", "-c", script, "fake-shim"}, Env: shimEnv()}
	spec := Spec{RunID: "impostor-1", RunDir: RunDir(dataDir, "impostor-1"), SocketPath: sock, Command: []string{"true"}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Start(ctx, l, spec)
	if c != nil {
		_ = c.Close()
	}
	require.Error(t, err, "Start took the impostor for its shim")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestDialStatusTakesTheShimAndRefusesAnotherProcess runs on Linux and macOS: the shim runs in
// this process, so the kernel must report this process as the socket's peer.
func TestDialStatusTakesTheShimAndRefusesAnotherProcess(t *testing.T) {
	if !peerPIDSupported {
		t.Skip("no peer check on this platform")
	}
	sh := startInProc(t, "sleep 30", nil)
	c, err := DialStatus(Status{RunID: sh.spec.RunID, Socket: sh.spec.SocketPath, ShimPID: os.Getpid()})
	require.NoError(t, err)
	st, err := c.Status()
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), st.ShimPID)
	_ = c.Close()

	_, err = DialStatus(Status{RunID: sh.spec.RunID, Socket: sh.spec.SocketPath, ShimPID: os.Getpid() + 1})
	require.ErrorIs(t, err, ErrWrongPeer)

	// The status the shim itself wrote must also be accepted.
	var own Status
	require.Eventually(t, func() bool {
		var err error
		own, err = ReadStatus(sh.spec.RunDir)
		return err == nil && own.Socket != ""
	}, 5*time.Second, 10*time.Millisecond)
	c, err = DialStatus(own)
	require.NoError(t, err)
	_ = c.Close()
}

// TestDialStatusWithoutAProcessIDFallsBackToThePath: a status with no shim PID is still reached,
// so a shim is never orphaned because its status lacks the field.
func TestDialStatusWithoutAProcessIDFallsBackToThePath(t *testing.T) {
	sh := startInProc(t, "sleep 30", nil)
	c, err := DialStatus(Status{Socket: sh.spec.SocketPath})
	require.NoError(t, err)
	_, err = c.Status()
	require.NoError(t, err)
	_ = c.Close()

	_, err = DialStatus(Status{ShimPID: os.Getpid()})
	require.Error(t, err)
}
