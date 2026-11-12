package runshim

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrivateDirNeedsTheOwnerAndTheMode: a socket directory another user made, or one others can
// enter, is refused.
func TestPrivateDirNeedsTheOwnerAndTheMode(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o700))
	fi, err := os.Lstat(dir)
	require.NoError(t, err)
	assert.NoError(t, privateDir(fi, os.Getuid()))
	assert.Error(t, privateDir(fi, os.Getuid()+1), "a directory another user owns")

	require.NoError(t, os.Chmod(dir, 0o755))
	fi, err = os.Lstat(dir)
	require.NoError(t, err)
	assert.Error(t, privateDir(fi, os.Getuid()), "a directory others can enter")

	file := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	fi, err = os.Lstat(file)
	require.NoError(t, err)
	assert.Error(t, privateDir(fi, os.Getuid()), "a regular file")

	target := t.TempDir()
	require.NoError(t, os.Chmod(target, 0o700))
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(target, link))
	fi, err = os.Lstat(link)
	require.NoError(t, err)
	assert.Error(t, privateDir(fi, os.Getuid()), "a symlink to a private directory")
}

// TestSocketPathRefusesAFallbackParentOthersCanEnter: with a long data dir the socket goes under
// <temp dir>/onett-<uid>/s. Both of those directories must be private, not only the last one.
func TestSocketPathRefusesAFallbackParentOthersCanEnter(t *testing.T) {
	short, err := os.MkdirTemp("", "sh")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	parent := filepath.Join(short, fmt.Sprintf("onett-%d", os.Getuid()))
	if len(filepath.Join(parent, "s", "12345678.sock")) > maxSocketPath {
		t.Skip("the temp directory is too long for the fallback socket path")
	}
	t.Setenv("TMPDIR", short)
	long := filepath.Join(t.TempDir(), strings.Repeat("d", 80))

	require.NoError(t, os.Mkdir(parent, 0o755))
	require.NoError(t, os.Chmod(parent, 0o755))
	_, err = SocketPath(long, "run-1")
	require.Error(t, err, "a parent directory others can enter")

	require.NoError(t, os.Chmod(parent, 0o700))
	p, err := SocketPath(long, "run-1")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(p, parent), "socket %s is not under %s", p, parent)
}
