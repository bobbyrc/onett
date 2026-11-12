package exec

import (
	"os"
	"testing"

	"github.com/bobbyrc/onett/internal/harness"
	"github.com/bobbyrc/onett/internal/runshim"
	"github.com/bobbyrc/onett/internal/runshim/shimtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const shimReexec = "ONETT_EXEC_TEST_RUNSHIM"

// TestMain lets the test binary act as `onett run-shim`, so no built onett is needed.
func TestMain(m *testing.M) {
	if os.Getenv(shimReexec) == "1" {
		os.Exit(shimtest.Main())
	}
	os.Exit(m.Run())
}

// TestPluginRunsThroughTheShim is the end-to-end check with limits.run_shim on: the same plugin
// run, started directly and behind a shim, gives the same events and result. (The fake harness
// runs in-process and never starts a child, so the exec harness stands in for it here.)
func TestPluginRunsThroughTheShim(t *testing.T) {
	spec := harness.Spec{RunID: "shim-e2e", Workdir: t.TempDir(), Prompt: "full task", Args: []string{"--spec"}}
	direct := newHarness(t, harness.Config{Command: pluginCommand(t), Args: []string{"--cfg"}})
	wantEvents, wantRes := run(t, direct, spec)

	dir := t.TempDir()
	// Stop the shim and wait for it before the temp directory is removed.
	t.Cleanup(func() { shimtest.StopAll(t, dir) })
	shimmed := newHarness(t, harness.Config{
		Command: pluginCommand(t), Args: []string{"--cfg"},
		Shim: &harness.ShimOptions{
			DataDir: dir,
			Launch: runshim.Launch{
				Command: []string{os.Args[0]},
				Env:     []string{"PATH=" + os.Getenv("PATH"), shimReexec + "=1", "GORACE=atexit_sleep_ms=0"},
			},
		},
	})
	gotEvents, gotRes := run(t, shimmed, spec)

	key := func(evs []harness.Event) (out []string) {
		for _, ev := range evs {
			out = append(out, string(ev.Kind)+"|"+ev.Text+"|"+ev.ToolID+"|"+ev.SessionID)
		}
		return out
	}
	require.NotEmpty(t, wantEvents)
	assert.Equal(t, key(wantEvents), key(gotEvents))
	assert.Equal(t, wantRes, gotRes)

	// The shim left its log behind for the daemon to read later.
	log, err := os.ReadFile(runshim.StreamPath(runshim.RunDir(dir, spec.RunID)))
	require.NoError(t, err)
	assert.NotEmpty(t, log)
}
