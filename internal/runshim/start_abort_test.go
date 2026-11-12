package runshim

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Start gives up on a shim that does not answer when its context ends. It must ask the shim to
// stop (SIGTERM) and wait for it: a shim told to stop kills the agent it may already have
// started, where a shim killed outright would leave that agent running with nothing watching it.
func TestStartStopsAShimItGivesUpOn(t *testing.T) {
	dir := t.TempDir()
	ready, stopped := filepath.Join(dir, "ready"), filepath.Join(dir, "stopped")
	// A stand-in shim that never opens the socket and records that it was asked to stop. The loop
	// is bounded so the script ends by itself if this test process dies.
	script := `trap 'echo term > "$STOPPED"; exit 0' TERM
echo up > "$READY"
i=0
while [ $i -lt 600 ]; do sleep 0.05; i=$((i+1)); done`
	l := Launch{
		Command: []string{"sh", "-c", script, "fake-shim"},
		Env:     append(shimEnv(), "READY="+ready, "STOPPED="+stopped),
	}
	spec := Spec{
		RunID: "abort-1", RunDir: RunDir(dir, "abort-1"), SocketPath: filepath.Join(dir, "x.sock"),
		Command: []string{"true"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		_, err := Start(ctx, l, spec)
		errc <- err
	}()
	require.Eventually(t, func() bool { _, err := os.Stat(ready); return err == nil },
		10*time.Second, 5*time.Millisecond, "the stand-in shim never started")
	cancel()
	select {
	case err := <-errc:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(20 * time.Second):
		t.Fatal("Start did not return after its context ended")
	}
	_, err := os.Stat(stopped)
	require.NoError(t, err, "the shim was asked to stop, and had stopped, before Start returned")
}
