package runshim

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A shim that is shutting down must not remove a socket a newer shim has bound at the same path.
// Closing a Go unix listener unlinks its socket file, which used to happen early in shutdown; a
// replacement shim (the harness starts one as soon as the file is gone) could then bind the path
// before the old shim's last step, and that step deleted the new shim's socket, so the new shim
// never answered.
func TestShutdownKeepsTheSocketUntilTheShimIsDone(t *testing.T) {
	dir := t.TempDir()
	// Short, because macOS limits socket paths to about 104 bytes and t.TempDir is long.
	short, err := os.MkdirTemp("", "sh")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	sock := filepath.Join(short, "s.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	logf, err := os.Create(filepath.Join(dir, "stream.log"))
	require.NoError(t, err)
	s := &shim{
		spec:  Spec{RunDir: dir, SocketPath: sock},
		ln:    ln,
		logf:  logf,
		conns: map[net.Conn]struct{}{},
	}

	// Hold the status lock so shutdown stalls on its final status write, as it does on a loaded
	// machine, after it has closed the listener and before it removes the socket file.
	s.statusMu.Lock()
	done := make(chan struct{})
	go func() { s.shutdown(); close(done) }()

	time.Sleep(100 * time.Millisecond)
	_, err = os.Stat(sock)
	require.NoError(t, err, "the socket file must stay until shutdown has finished its other work")

	s.statusMu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	_, err = os.Stat(sock)
	require.True(t, os.IsNotExist(err), "shutdown removes the socket last: %v", err)
}

// A shim whose socket path was taken over by a newer shim (the harness gives up waiting for an
// old shim after a few seconds) must leave the newer shim's socket alone.
func TestShutdownLeavesAReplacementsSocket(t *testing.T) {
	dir := t.TempDir()
	short, err := os.MkdirTemp("", "sh")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	sock := filepath.Join(short, "s.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	logf, err := os.Create(filepath.Join(dir, "stream.log"))
	require.NoError(t, err)
	s := &shim{spec: Spec{RunDir: dir, SocketPath: sock}, ln: ln, logf: logf, conns: map[net.Conn]struct{}{}}
	s.sockInfo, err = os.Lstat(sock)
	require.NoError(t, err)

	// The old shim closes its listener; a replacement then binds the same path.
	ul := ln.(*net.UnixListener)
	ul.SetUnlinkOnClose(false)
	require.NoError(t, ln.Close())
	// Keep the old inode alive through a hard link: otherwise the filesystem may hand the freed
	// inode number straight to the replacement socket, and the shim could not tell the two apart.
	require.NoError(t, os.Link(sock, filepath.Join(short, "old.sock")))
	require.NoError(t, os.Remove(sock))
	newer, err := net.Listen("unix", sock)
	require.NoError(t, err)
	defer newer.Close()

	s.shutdown()
	c, err := net.DialTimeout("unix", sock, time.Second)
	require.NoError(t, err, "the replacement's socket must survive the old shim's shutdown")
	_ = c.Close()
}
