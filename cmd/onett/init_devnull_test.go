package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `onett init < /dev/null` is how a script or a service manager runs it. /dev/null is a character
// device, as a terminal is, but nobody is there to answer.
func TestInitWithDevNullOnStdinAsksNothing(t *testing.T) {
	null, err := os.Open(os.DevNull)
	require.NoError(t, err)
	old := os.Stdin
	os.Stdin = null
	t.Cleanup(func() {
		os.Stdin = old
		_ = null.Close()
	})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ONETT_CONFIG", "")
	cfg := filepath.Join(home, "onett.yaml")

	code, out, errOut := runCLI("init", "--config", cfg)
	require.Equal(t, 0, code, errOut)
	assert.NotContains(t, out, "How should onett reach you")
	assert.NotContains(t, out, "Choice")
	assert.Contains(t, out, "onett doctor", "the warning that onett cannot reach the owner")
	assert.FileExists(t, cfg)
}

// Nothing but a terminal may be asked a question. A real terminal cannot be opened in a test, so
// this covers the three things stdin is when nobody is there.
func TestIsTerminalIsFalseWithoutATerminal(t *testing.T) {
	null, err := os.Open(os.DevNull)
	require.NoError(t, err)
	defer null.Close()
	assert.False(t, isTerminal(null), "the null device")

	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	defer w.Close()
	assert.False(t, isTerminal(r), "a pipe")

	file, err := os.Create(filepath.Join(t.TempDir(), "stdin"))
	require.NoError(t, err)
	defer file.Close()
	assert.False(t, isTerminal(file), "a regular file")
}
