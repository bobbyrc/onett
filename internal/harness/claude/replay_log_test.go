package claude

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/harness"
	"github.com/bobbyrc/onett/internal/runshim"
)

// A stream.log that ends in a result line, the usual state of a run that finished a turn just
// before the daemon died, must replay without waiting for a process that is not there.
func TestReplayLogHandlesResultLine(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(runshim.RunDir(dir, "r1"), 0o755))
	log := `{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]},"session_id":"s1"}` + "\n" +
		`{"type":"result","is_error":false,"result":"done","session_id":"s1","total_cost_usd":0.1,"usage":{"input_tokens":1,"output_tokens":2}}` + "\n"
	require.NoError(t, os.WriteFile(runshim.StreamPath(runshim.RunDir(dir, "r1")), []byte(log), 0o644))

	r := &runner{cfg: harness.Config{Shim: &harness.ShimOptions{DataDir: dir}}}
	type outcome struct {
		evs []harness.Event
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		evs, err := r.ReplayLog(harness.Spec{RunID: "r1"}, 0)
		done <- outcome{evs, err}
	}()
	select {
	case o := <-done:
		require.NoError(t, o.err)
		require.NotEmpty(t, o.evs)
		assert.Equal(t, "s1", o.evs[0].SessionID)
	case <-time.After(5 * time.Second):
		t.Fatal("ReplayLog blocked on a result line")
	}
}
