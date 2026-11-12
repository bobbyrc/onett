package sqlite

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestBackupRefusesLeftoverPartialFile(t *testing.T) {
	src := filepath.Join(t.TempDir(), "onett.db")
	s, err := Open(src)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	dst := filepath.Join(t.TempDir(), "copy.db")
	require.NoError(t, os.WriteFile(dst+".partial", []byte("old"), 0o600))

	err = Backup(context.Background(), src, dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "interrupted backup")
	assert.NoFileExists(t, dst)

	leftover, err := os.ReadFile(dst + ".partial")
	require.NoError(t, err)
	assert.Equal(t, "old", string(leftover))
}

// makeBackupSource creates a database with one task and closes it, so a test can back it up.
func makeBackupSource(t *testing.T) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "onett.db")
	s, err := Open(src)
	require.NoError(t, err)
	_, err = s.CreateTask(context.Background(), domain.Task{Project: "p", Title: "t", Status: domain.TaskQueued})
	require.NoError(t, err)
	require.NoError(t, s.Close())
	return src
}

func TestBackupLeavesTheSourceFileUnchanged(t *testing.T) {
	src := makeBackupSource(t)

	before, err := os.ReadFile(src)
	require.NoError(t, err)

	dst := filepath.Join(t.TempDir(), "copy.db")
	require.NoError(t, Backup(context.Background(), src, dst))

	after, err := os.ReadFile(src)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(before, after), "a backup never writes the database file")
}

func TestBackupCancelledLeavesNoFile(t *testing.T) {
	src := makeBackupSource(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dst := filepath.Join(t.TempDir(), "copy.db")
	err := Backup(ctx, src, dst)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.NoFileExists(t, dst)
	assert.NoFileExists(t, dst+".partial")
}
