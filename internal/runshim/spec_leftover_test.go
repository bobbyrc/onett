package runshim

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The spec file holds the agent's environment. The shim removes it once read; when the shim
// never gets that far, Start must not leave it behind.

// TestStartRemovesTheSpecWhenItGivesUp: a shim that never reads its spec and is told to stop.
func TestStartRemovesTheSpecWhenItGivesUp(t *testing.T) {
	dir := t.TempDir()
	ready, stopped := filepath.Join(dir, "ready"), filepath.Join(dir, "stopped")
	script := `trap 'echo term > "$STOPPED"; exit 0' TERM
echo up > "$READY"
i=0
while [ $i -lt 600 ]; do sleep 0.05; i=$((i+1)); done`
	l := Launch{
		Command: []string{"sh", "-c", script, "fake-shim"},
		Env:     append(shimEnv(), "READY="+ready, "STOPPED="+stopped),
	}
	spec := Spec{
		RunID: "leftover-1", RunDir: RunDir(dir, "leftover-1"), SocketPath: filepath.Join(dir, "x.sock"),
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
	_, err := os.Stat(filepath.Join(spec.RunDir, "spec.json"))
	require.True(t, os.IsNotExist(err), "spec.json holds the agent's environment and was left on disk")
}

// TestStartRemovesTheSpecWhenTheShimCannotStart: the shim binary does not exist.
func TestStartRemovesTheSpecWhenTheShimCannotStart(t *testing.T) {
	l := Launch{Command: []string{"/nonexistent/onett-shim"}, Env: shimEnv()}
	dir := t.TempDir()
	spec := Spec{
		RunID: "leftover-2", RunDir: RunDir(dir, "leftover-2"), SocketPath: filepath.Join(dir, "x.sock"),
		Command: []string{"true"},
	}
	_, err := Start(context.Background(), l, spec)
	require.Error(t, err)
	_, err = os.Stat(filepath.Join(spec.RunDir, "spec.json"))
	require.True(t, os.IsNotExist(err), "spec.json holds the agent's environment and was left on disk")
}
