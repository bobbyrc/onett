package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCodesignScript runs scripts/codesign.sh with a stub codesign on PATH that records its
// arguments, and returns those arguments (empty when the stub never ran) and stderr.
func runCodesignScript(t *testing.T, identity string) (called, stderr string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	dir := t.TempDir()
	record := filepath.Join(dir, "args")
	stub := "#!/bin/sh\necho \"$@\" >" + record + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "codesign"), []byte(stub), 0o755))

	script, err := filepath.Abs("../../scripts/codesign.sh")
	require.NoError(t, err)
	cmd := exec.Command("bash", script, "/tmp/onett-binary")
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "ONETT_CODESIGN_IDENTITY="+identity)
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	require.NoError(t, cmd.Run())
	b, _ := os.ReadFile(record)
	return strings.TrimSpace(string(b)), errBuf.String()
}

func TestCodesignScriptSignsOnlyWithIdentity(t *testing.T) {
	called, _ := runCodesignScript(t, "My Cert")
	assert.Equal(t, "--force --sign My Cert --identifier io.onett.onett /tmp/onett-binary", called)

	called, stderr := runCodesignScript(t, "")
	assert.Empty(t, called, "codesign must not run without an identity")
	if runtime.GOOS == "darwin" {
		assert.Contains(t, stderr, "ONETT_CODESIGN_IDENTITY")
	}
}
