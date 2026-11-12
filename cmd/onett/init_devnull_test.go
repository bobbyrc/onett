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
