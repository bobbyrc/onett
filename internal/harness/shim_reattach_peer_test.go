package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/runshim"
	"github.com/stretchr/testify/require"
)

// detachedShim starts an agent behind a shim, lets go of it the way a stopping onett does, and
// returns what a restarted onett has: the spec to reattach with, the run directory and the
// status the shim wrote. The agent prints nothing until the file `release` exists, so the shim
// does not rewrite status.json in between; then it prints "after" and sleeps.
func detachedShim(t *testing.T) (spec ProcSpec, runDir string, st runshim.Status, release string) {
	t.Helper()
	opts := testShimOptions(t)
	release = filepath.Join(t.TempDir(), "go")
	runID := "run-" + strings.ReplaceAll(t.Name(), "/", "-")
	// The loop is bounded, so the agent ends by itself if the test stops early.
	script := fmt.Sprintf("i=0; while [ ! -e '%s' ] && [ $i -lt 600 ]; do sleep 0.05; i=$((i+1)); done; echo after; sleep 30", release)
	spec = ProcSpec{RunID: runID, Shim: opts, Command: []string{"sh", "-c", script}, ParseLine: textParser}

	p, err := StartProcess(context.Background(), spec)
	require.NoError(t, err)
	require.True(t, p.Detach())
	_, err = p.Wait()
	require.ErrorIs(t, err, ErrDetached)

	runDir = runshim.RunDir(opts.DataDir, runID)
	st, err = runshim.ReadStatus(runDir)
	require.NoError(t, err)
	require.Positive(t, st.ShimPID)
	require.NotEmpty(t, st.Socket)

	// Restore the real status before the shim is stopped, so the stop finds the right file.
	orig, err := os.ReadFile(runshim.StatusPath(runDir))
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.WriteFile(runshim.StatusPath(runDir), orig, 0o600) })
	return spec, runDir, st, release
}

// writeStatusAsMain writes status.json the way the shim on main writes it: these keys, this
// order, two-space indent.
func writeStatusAsMain(t *testing.T, runDir string, st runshim.Status, shimPID int) {
	t.Helper()
	body := fmt.Sprintf(`{
  "run_id": %q,
  "version": 1,
  "shim_pid": %d,
  "agent_pid": %d,
  "agent_pgid": %d,
  "socket": %q,
  "started_at": %q,
  "last_output_at": %q,
  "exited": false,
  "exit_code": 0,
  "log_size": 0,
  "acked": 0
}
`, st.RunID, shimPID, st.AgentPID, st.AgentPGID, st.Socket,
		st.StartedAt.Format(time.RFC3339Nano), st.LastOutputAt.Format(time.RFC3339Nano))
	require.NoError(t, os.WriteFile(runshim.StatusPath(runDir), []byte(body), 0o600))
}

// expectAfter releases the agent and waits for the line it then prints, proving the reattached
// process reads new output; then it stops the agent and waits for it.
func expectAfter(t *testing.T, p *Proc, release string) {
	t.Helper()
	require.NoError(t, os.WriteFile(release, nil, 0o600))
	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev, ok := <-p.Events():
			if !ok {
				t.Fatal("no output after reattaching")
			}
			if ev.Text == "after" {
				p.Cancel()
				drain(p)
				_, _ = p.Wait()
				return
			}
		case <-deadline:
			t.Fatal("no output after reattaching")
		}
	}
}

// A daemon update must not orphan a running agent: a shim started by the binary on main is found
// again from the status file that binary writes.
func TestReattachTakesAShimWhoseStatusIsAsMainWritesIt(t *testing.T) {
	spec, runDir, st, release := detachedShim(t)
	writeStatusAsMain(t, runDir, st, st.ShimPID)
	p, err := ReattachProcess(context.Background(), spec, 0)
	require.NoError(t, err)
	expectAfter(t, p, release)
}

// The status names one process; when the kernel says another answers on the socket, it is not
// taken for the shim.
func TestReattachRefusesAnotherProcessOnTheSocket(t *testing.T) {
	spec, runDir, st, _ := detachedShim(t)
	writeStatusAsMain(t, runDir, st, os.Getpid()) // this test process is alive and is not the shim
	p, err := ReattachProcess(context.Background(), spec, 0)
	if p != nil {
		p.Detach()
	}
	require.ErrorIs(t, err, runshim.ErrWrongPeer)
}

// A status without a shim PID cannot be checked. Its shim is still reattached, never refused:
// refusing would leave the agent running with nobody watching.
func TestReattachStillTakesAShimWhoseStatusHasNoProcessID(t *testing.T) {
	spec, runDir, st, release := detachedShim(t)
	writeStatusAsMain(t, runDir, st, 0)
	p, err := ReattachProcess(context.Background(), spec, 0)
	require.NoError(t, err)
	expectAfter(t, p, release)
}

// TestReattachWithoutAStatusFileFails: a run whose shim never wrote a status has no shim to
// reach, and reattaching says so instead of guessing a socket path.
func TestReattachWithoutAStatusFileFails(t *testing.T) {
	spec := ProcSpec{RunID: "never-started", Shim: testShimOptions(t), Command: []string{"true"}, ParseLine: textParser}
	p, err := ReattachProcess(context.Background(), spec, 0)
	if p != nil {
		p.Detach()
	}
	require.Error(t, err)
}
