package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
)

func TestHarnessCLIFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty: nothing is found

	assert.True(t, harnessCLIFound(config.Harness{Type: "fake"}), "a harness with no program is always available")
	assert.False(t, harnessCLIFound(config.Harness{Type: "claude-code"}))
	assert.False(t, harnessCLIFound(config.Harness{Type: "codex"}))
	assert.False(t, harnessCLIFound(config.Harness{Type: "exec"}), "no command")
	assert.False(t, harnessCLIFound(config.Harness{Type: "exec", Command: []string{"nope"}}))
}

func TestHarnessCLIFoundOnPath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", dir)

	assert.True(t, harnessCLIFound(config.Harness{Type: "claude-code"}))
	assert.True(t, harnessCLIFound(config.Harness{Type: "exec", Command: []string{"claude"}}))
	assert.False(t, harnessCLIFound(config.Harness{Type: "codex"}))
}
