package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	notFoundOut = `Could not find service "io.onett.serve" in domain for user gui: 501`
	busyOut     = "Bootstrap failed: 5: Input/output error"
)

// outdatedPlist writes an old plist so install takes the bootout-then-bootstrap path.
func outdatedPlist(t *testing.T, dir string) string {
	t.Helper()
	plist := filepath.Join(dir, "io.onett.serve.plist")
	require.NoError(t, os.WriteFile(plist, []byte("<plist>old</plist>"), 0o600))
	return plist
}

func countCalls(calls []runCall, verb string) int {
	n := 0
	for _, c := range calls {
		if c.args[0] == verb {
			n++
		}
	}
	return n
}

func TestInstallWaitsForBootoutToFinish(t *testing.T) {
	dir := stubHost(t)
	cfg := writeServiceConfig(t, filepath.Join(t.TempDir(), "data"))
	plist := outdatedPlist(t, dir)
	prints := 0
	calls := fakeRunner(t, func(name string, args ...string) (string, error) {
		if args[0] == "print" {
			prints++
			if prints <= 2 { // the job is still being removed
				return "service = {}", nil
			}
			return notFoundOut, errors.New("exit status 113")
		}
		return "", nil
	})

	code, _, errOut := runCLI("service", "install", "--config", cfg)
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, 3, countCalls(*calls, "print"))
	last := (*calls)[len(*calls)-1]
	assert.Equal(t, runCall{name: "launchctl", args: []string{"bootstrap", "gui/501", plist}}, last, "bootstrap comes after the job is gone")
	assert.Equal(t, 1, countCalls(*calls, "bootstrap"))
}

func TestInstallRetriesBootstrapOnIOError(t *testing.T) {
	dir := stubHost(t)
	cfg := writeServiceConfig(t, filepath.Join(t.TempDir(), "data"))
	outdatedPlist(t, dir)
	var waits []time.Duration
	pause = func(d time.Duration) { waits = append(waits, d) }
	boots := 0
	calls := fakeRunner(t, func(name string, args ...string) (string, error) {
		switch args[0] {
		case "print":
			return notFoundOut, errors.New("exit status 113")
		case "bootstrap":
			boots++
			if boots <= 2 {
				return busyOut, errors.New("exit status 5")
			}
		}
		return "", nil
	})

	code, _, errOut := runCLI("service", "install", "--config", cfg)
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, 3, countCalls(*calls, "bootstrap"))
	assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second}, waits)
}

func TestInstallGivesUpAfterFiveBusyBootstraps(t *testing.T) {
	dir := stubHost(t)
	cfg := writeServiceConfig(t, filepath.Join(t.TempDir(), "data"))
	plist := outdatedPlist(t, dir)
	calls := fakeRunner(t, func(name string, args ...string) (string, error) {
		switch args[0] {
		case "print":
			return notFoundOut, errors.New("exit status 113")
		case "bootstrap":
			return busyOut, errors.New("exit status 5")
		}
		return "", nil
	})

	code, _, errOut := runCLI("service", "install", "--config", cfg)
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "Input/output error")
	assert.Equal(t, 5, countCalls(*calls, "bootstrap"))
	body, err := os.ReadFile(plist)
	require.NoError(t, err)
	assert.Contains(t, string(body), "AbandonProcessGroup", "the new plist stays on disk")
}

func TestInstallDoesNotRetryOtherBootstrapErrors(t *testing.T) {
	dir := stubHost(t)
	cfg := writeServiceConfig(t, filepath.Join(t.TempDir(), "data"))
	outdatedPlist(t, dir)
	calls := fakeRunner(t, func(name string, args ...string) (string, error) {
		switch args[0] {
		case "print":
			return notFoundOut, errors.New("exit status 113")
		case "bootstrap":
			return "Bootstrap failed: 125: Domain does not support specified action", errors.New("exit status 125")
		}
		return "", nil
	})

	code, _, errOut := runCLI("service", "install", "--config", cfg)
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "Domain does not support")
	assert.Equal(t, 1, countCalls(*calls, "bootstrap"))
}

func TestInstallBootstrapsAnInstalledButUnloadedService(t *testing.T) {
	dir := stubHost(t)
	cfg := writeServiceConfig(t, filepath.Join(t.TempDir(), "data"))
	calls := fakeRunner(t, nil)
	code, _, errOut := runCLI("service", "install", "--config", cfg)
	require.Equal(t, 0, code, errOut)

	// Same plist on disk, but launchd no longer has the job (an earlier bootstrap failed).
	*calls = nil
	fakeRunner2 := fakeRunner(t, func(name string, args ...string) (string, error) {
		if args[0] == "print" {
			return notFoundOut, errors.New("exit status 113")
		}
		return "", nil
	})
	code, _, errOut = runCLI("service", "install", "--config", cfg)
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, 0, countCalls(*fakeRunner2, "kickstart"))
	assert.Equal(t, []runCall{
		{name: "launchctl", args: []string{"print", "gui/501/io.onett.serve"}},
		{name: "launchctl", args: []string{"bootstrap", "gui/501", filepath.Join(dir, "io.onett.serve.plist")}},
	}, *fakeRunner2)
}
