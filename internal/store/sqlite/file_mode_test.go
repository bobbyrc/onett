package sqlite

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenCreatesAPrivateDatabase: a new database, and the -wal and -shm files SQLite makes beside
// it, are readable by their owner only, whatever the process's default file mode is.
func TestOpenCreatesAPrivateDatabase(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })

	path := filepath.Join(t.TempDir(), "onett.db")
	s, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		fi, err := os.Stat(p)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "%s must be readable by its owner only", p)
	}
}

// TestOpenTightensAnExistingDatabase: a database an older onett left readable by everyone is
// made private when it is opened again, with any -wal and -shm files left beside it.
func TestOpenTightensAnExistingDatabase(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })

	path := filepath.Join(t.TempDir(), "onett.db")
	s, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	require.NoError(t, os.Chmod(path, 0o644))
	for _, p := range []string{path + "-wal", path + "-shm"} {
		require.NoError(t, os.WriteFile(p, nil, 0o644))
		require.NoError(t, os.Chmod(p, 0o644))
	}

	s, err = Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		fi, err := os.Stat(p)
		require.NoError(t, err, "%s must exist", p)
		assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "%s must be readable by its owner only", p)
	}
}
