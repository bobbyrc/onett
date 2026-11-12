package runshim

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// longDataDir is a data directory too long to hold its own socket.
const longDataDir = "deep-directory-name/deep-directory-name/deep-directory-name/deep-directory-name/" +
	"deep-directory-name/deep-directory-name/deep-directory-name/deep-directory-name"

// Data directories too long to hold their own socket share one fallback directory, so there the
// socket name must depend on the data directory as well as the run ID. With the run ID alone, two
// daemons (or two test runs) that use the same run ID met on one socket, and the second shim
// failed with "socket is in use" or talked to the other one's agent.
func TestSocketPathDiffersBetweenDataDirsThatShareTheFallback(t *testing.T) {
	a, b := filepath.Join(t.TempDir(), longDataDir), filepath.Join(t.TempDir(), longDataDir)
	pa, err := SocketPath(a, "run-1")
	require.NoError(t, err)
	pb, err := SocketPath(b, "run-1")
	require.NoError(t, err)
	require.Equal(t, filepath.Dir(pa), filepath.Dir(pb), "both fall back to the shared directory")
	assert.NotEqual(t, pa, pb, "the same run ID in another data directory gets another socket")

	again, err := SocketPath(a, "run-1")
	require.NoError(t, err)
	assert.Equal(t, pa, again, "the name is stable, so a restarted daemon finds the socket again")
}

// In the fallback directory a relative data directory names the same socket as its absolute form.
func TestSocketPathIsTheSameForARelativeDataDir(t *testing.T) {
	t.Chdir(t.TempDir())
	wd, err := os.Getwd()
	require.NoError(t, err)
	rel, err := SocketPath(longDataDir, "run-1")
	require.NoError(t, err)
	abs, err := SocketPath(filepath.Join(wd, longDataDir), "run-1")
	require.NoError(t, err)
	assert.Equal(t, abs, rel)
	assert.False(t, strings.HasPrefix(rel, longDataDir), "the long directory fell back: %s", rel)
}

// A socket inside the data directory keeps the name it always had, from the run ID alone, so an
// agent that was started before this name changed for the fallback is still found.
func TestSocketPathInsideTheDataDirDependsOnTheRunIDOnly(t *testing.T) {
	// Short, because macOS limits socket paths to about 104 bytes and t.TempDir is long.
	short, err := os.MkdirTemp("", "sh")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	sum := sha256.Sum256([]byte("run-1"))
	want := filepath.Join(short, "s", hex.EncodeToString(sum[:])[:8]+".sock")
	if len(want) > maxSocketPath {
		t.Skipf("the temp directory is too long for a socket of its own: %s", short)
	}
	got, err := SocketPath(short, "run-1")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}
