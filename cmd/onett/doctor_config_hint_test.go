package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoctorConfigHintDoesNotSuggestInitForExistingFile(t *testing.T) {
	healthyDaemon(t)
	toolRunner(t, nil)
	path := filepath.Join(t.TempDir(), "onett.yaml")
	require.NoError(t, os.WriteFile(path, []byte("agents:\n  senior-engineer:\n    harness: codex\n"), 0o600))

	code, out, _ := runCLI("doctor", "--config", path)
	assert.Equal(t, 1, code, out)
	assertLine(t, out, "FAIL", "config: ")
	assertLine(t, out, "FAIL", "fix the settings the message names in "+path)
	assert.NotContains(t, out, "onett init")
}

func TestDoctorConfigHintKeepsInitForMissingFile(t *testing.T) {
	healthyDaemon(t)
	toolRunner(t, nil)
	missing := filepath.Join(t.TempDir(), "absent.yaml")

	code, out, _ := runCLI("doctor", "--config", missing)
	assert.Equal(t, 1, code, out)
	assertLine(t, out, "FAIL", "run `onett init` to create one")
	assert.NotContains(t, out, "fix the settings the message names")
}
