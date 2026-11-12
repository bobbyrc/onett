package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cloneRunner fakes the tools as toolRunner does, but answers `git ... remote ...` with the given
// output and error. It returns the recorded calls.
func cloneRunner(t *testing.T, remoteOut string, remoteErr error) *[]runCall {
	t.Helper()
	return fakeRunner(t, func(name string, args ...string) (string, error) {
		if name == "git" && slices.Contains(args, "remote") {
			return remoteOut, remoteErr
		}
		switch name {
		case "git":
			return "git version 2.45.0\n", nil
		case "gh":
			return "github.com\n", nil
		case "claude":
			return "2.1.0 (Claude Code)\n", nil
		case "codex":
			return "codex-cli 0.9.0\n", nil
		}
		return "", errors.New("unexpected command " + name)
	})
}

func TestDoctorCloneFailsWithoutOrigin(t *testing.T) {
	healthyDaemon(t)
	f := newDoctorFixture(t, "")
	require.NoError(t, os.MkdirAll(filepath.Join(f.clone, ".git"), 0o755))
	calls := cloneRunner(t, "error: No such remote 'origin'\n", errors.New("exit status 2"))

	code, out, errOut := runCLI("doctor", "--config", f.cfg)
	assert.Equal(t, 1, code, out+errOut)
	assertLine(t, out, "FAIL", "project onett: clone at "+f.clone+" has no usable origin remote (error: No such remote 'origin')")
	assertLine(t, out, "FAIL", "remote add origin")

	found := false
	for _, c := range *calls {
		if c.name == "git" && slices.Contains(c.args, "remote") {
			found = true
			assert.Equal(t, []string{"-c", "core.hooksPath=/dev/null", "-C", f.clone, "remote", "get-url", "origin"}, c.args)
		}
	}
	assert.True(t, found, "doctor should ask git for the origin remote")
}

func TestDoctorCloneOKWithOriginAndHidesURL(t *testing.T) {
	healthyDaemon(t)
	f := newDoctorFixture(t, "")
	require.NoError(t, os.MkdirAll(filepath.Join(f.clone, ".git"), 0o755))
	cloneRunner(t, "https://user:sekret-token@github.com/o/r.git\n", nil)

	code, out, _ := runCLI("doctor", "--config", f.cfg)
	assert.Equal(t, 0, code, out)
	assertLine(t, out, "ok", "project onett: clone at "+f.clone)
	assert.NotContains(t, out, "sekret-token")
}

func TestDoctorCloneFailsForNonEmptyNonCheckout(t *testing.T) {
	healthyDaemon(t)
	f := newDoctorFixture(t, "")
	require.NoError(t, os.RemoveAll(filepath.Join(f.clone, ".git")))
	require.NoError(t, os.WriteFile(filepath.Join(f.clone, "notes.txt"), []byte("hi"), 0o600))
	calls := cloneRunner(t, "", nil)

	code, out, _ := runCLI("doctor", "--config", f.cfg)
	assert.Equal(t, 1, code, out)
	assertLine(t, out, "FAIL", "project onett: "+f.clone+" is not a git checkout and is not empty")
	for _, c := range *calls {
		assert.False(t, c.name == "git" && slices.Contains(c.args, "remote"), "no remote check for a non-checkout")
	}
}

func TestDoctorCloneAcceptsEmptyDirectory(t *testing.T) {
	healthyDaemon(t)
	f := newDoctorFixture(t, "")
	require.NoError(t, os.RemoveAll(filepath.Join(f.clone, ".git")))
	cloneRunner(t, "", nil)

	code, out, _ := runCLI("doctor", "--config", f.cfg)
	assert.Equal(t, 0, code, out)
	assertLine(t, out, "ok", "project onett: "+f.clone+" is empty; it will be cloned there")
}
