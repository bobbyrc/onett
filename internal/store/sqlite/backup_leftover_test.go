package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
