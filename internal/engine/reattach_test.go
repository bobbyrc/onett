package engine

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/harness"
	_ "github.com/bobbyrc/onett/internal/harness/exec" // registers the "exec" harness type
	"github.com/bobbyrc/onett/internal/runshim"
	"github.com/bobbyrc/onett/internal/runshim/shimtest"
	"github.com/bobbyrc/onett/internal/scm"
	scmfake "github.com/bobbyrc/onett/internal/scm/fake"
	"github.com/bobbyrc/onett/internal/store"
	"github.com/bobbyrc/onett/internal/store/sqlite"
	"github.com/bobbyrc/onett/internal/workspace"
)

// shimReexec makes the test binary act as `onett run-shim`, so these tests need no built onett.
const shimReexec = "ONETT_ENGINE_TEST_RUNSHIM"

func TestMain(m *testing.M) {
	if os.Getenv(shimReexec) == "1" {
		os.Exit(shimtest.Main())
	}
	os.Exit(m.Run())
}

// The engine tests for reattaching use the exec harness with a shell script as the agent (the
// fake harness runs in-process, so it has no child for a shim to keep alive). The script prints
// two lines, waits for the gate file, then reports no_changes and prints two more lines.
const reattachAgent = `#!/bin/sh
cat > /dev/null
echo '{"type":"text","text":"line one","session_id":"s1"}'
echo '{"type":"text","text":"line two"}'
while [ ! -e "$GATE_FILE" ]; do sleep 0.05; done
echo '{"type":"text","text":"line three"}'
printf '{"status":"no_changes","summary":"nothing to do"}' > "$ONETT_OUTBOX/result.json"
echo '{"type":"text","text":"line four"}'
echo '{"type":"result","session_id":"s1","text":"done"}'
`

// fakeClock is a settable clock for the engine's Now.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

// reattachFixture is one data directory, store and repository shared by successive engines, which
// stand for successive daemon processes.
type reattachFixture struct {
	t       *testing.T
	dataDir string
	gate    string
	st      store.Store
	scm     *scmfake.Fake
	cfg     *config.Config
}

func newReattachFixture(t *testing.T, stall, runTimeout time.Duration) *reattachFixture {
	t.Helper()
	root := t.TempDir()
	f := &reattachFixture{t: t, dataDir: filepath.Join(root, "data"), gate: filepath.Join(root, "gate")}
	// Registered right after TempDir, so it runs after the engines have stopped and just before the
	// directory is removed (cleanups run last-in first-out). A test that ends early, or a shim that
	// has not yet seen its ack, would otherwise leave a process running, and still writing into the
	// directory while TempDir removes it ("directory not empty").
	t.Cleanup(func() { shimtest.StopAll(t, f.dataDir) })
	bare, clone := filepath.Join(root, "remote.git"), filepath.Join(root, "clone")
	rtSeedRepo(t, bare)
	agent := filepath.Join(root, "agent.sh")
	require.NoError(t, os.WriteFile(agent, []byte(reattachAgent), 0o755))

	var agents strings.Builder
	for _, a := range []string{"orchestrator", "implementer", "reviewer", "triager", "sre", "improver"} {
		fmt.Fprintf(&agents, "  %s: {harness: plug, prompt: builtin:%s}\n", a, a)
	}
	doc := fmt.Sprintf(`server:
  data_dir: %s
limits:
  stall_timeout: %s
  run_timeout: %s
  max_attempts: 1
harnesses:
  plug: {type: exec, command: [sh, %s]}
agents:
%sprojects:
  demo:
    repo: %s
    path: %s
    default_branch: main
    env: {GATE_FILE: %s}
`, f.dataDir, stall, runTimeout, agent, agents.String(), bare, clone, f.gate)
	cfg, err := config.Parse([]byte(doc), "")
	require.NoError(t, err, doc)
	f.cfg = cfg

	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	f.st = st
	f.scm = scmfake.New()
	return f
}

// engine builds a new engine on the fixture and runs it. stop cancels it and waits for Run to
// return, as a daemon exit does; the shim and the agent are left running.
func (f *reattachFixture) engine(now func() time.Time) (e *Engine, stop func()) {
	f.t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	e, err := New(Deps{
		Config:     f.cfg,
		Store:      f.st,
		Bus:        events.NewBus(f.st, logger),
		Workspaces: workspace.New(filepath.Join(f.dataDir, "worktrees")),
		Logger:     logger,
		SCMFor:     func(*config.Project) (scm.SCM, error) { return f.scm, nil },
		HarnessFor: func(name string, h config.Harness) (harness.Harness, error) {
			return harness.New(harness.Config{
				Name: name, Type: h.Type, Command: h.Command, Args: h.Args, Env: h.Env,
				Shim: &harness.ShimOptions{
					DataDir: f.dataDir,
					Launch: runshim.Launch{
						Command: []string{os.Args[0]},
						// The race detector otherwise sleeps a second when the shim exits.
						Env: []string{"PATH=" + os.Getenv("PATH"), shimReexec + "=1", "GORACE=atexit_sleep_ms=0"},
					},
				},
			})
		},
		Now:           now,
		SchedulerTick: 50 * time.Millisecond,
	})
	require.NoError(f.t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				require.NoError(f.t, err)
			case <-time.After(30 * time.Second):
				f.t.Fatal("engine did not stop")
			}
		})
	}
	f.t.Cleanup(stop)
	return e, stop
}

func (f *reattachFixture) create(e *Engine) domain.Task {
	f.t.Helper()
	task, err := e.createTask(context.Background(), domain.Task{Project: "demo", Title: "Reattach me", Body: "do it"})
	require.NoError(f.t, err)
	return task
}

func (f *reattachFixture) task(id string) domain.Task {
	f.t.Helper()
	got, err := f.st.GetTask(context.Background(), id)
	require.NoError(f.t, err)
	return got
}

func (f *reattachFixture) run(taskID string) domain.Run {
	f.t.Helper()
	runs, err := f.st.ListRuns(context.Background(), store.RunFilter{TaskID: taskID})
	require.NoError(f.t, err)
	require.Len(f.t, runs, 1)
	return runs[0]
}

func (f *reattachFixture) logTexts(runID string) []string {
	f.t.Helper()
	logs, err := f.st.ListRunLogs(context.Background(), runID, 0, 1000)
	require.NoError(f.t, err)
	var out []string
	for _, l := range logs {
		if l.Kind == domain.LogText {
			out = append(out, l.Text)
		}
	}
	return out
}

func (f *reattachFixture) waitTask(id string, want domain.TaskStatus) domain.Task {
	f.t.Helper()
	var got domain.Task
	rtWait(f.t, 30*time.Second, "task to reach "+string(want), func() bool {
		got = f.task(id)
		return got.Status == want
	})
	return got
}

// waitShimGone waits for the run's shim and agent processes to exit, so nothing is still writing
// into the fixture's directory when the test ends.
func (f *reattachFixture) waitShimGone(runID string) {
	f.t.Helper()
	st, err := runshim.ReadStatus(runshim.RunDir(f.dataDir, runID))
	require.NoError(f.t, err)
	rtWait(f.t, 15*time.Second, "shim to exit", func() bool {
		return syscall.Kill(st.ShimPID, 0) == syscall.ESRCH
	})
}

func (f *reattachFixture) shimStatus(runID string) runshim.Status {
	f.t.Helper()
	st, err := runshim.ReadStatus(runshim.RunDir(f.dataDir, runID))
	require.NoError(f.t, err)
	return st
}

func (f *reattachFixture) streamSize(runID string) int64 {
	f.t.Helper()
	fi, err := os.Stat(runshim.StreamPath(runshim.RunDir(f.dataDir, runID)))
	require.NoError(f.t, err)
	return fi.Size()
}

// waitStoredBothLines waits until the run's first two agent lines are in the log and the stream
// offset past them is stored, then returns the run.
func (f *reattachFixture) waitStoredBothLines(taskID string) domain.Run {
	f.t.Helper()
	var run domain.Run
	defer func() {
		if f.t.Failed() {
			tk := f.task(taskID)
			runs, _ := f.st.ListRuns(context.Background(), store.RunFilter{TaskID: taskID})
			f.t.Logf("task %s %q; runs %+v", tk.Status, tk.BlockedReason, runs)
			for _, r := range runs {
				f.t.Logf("texts %q", f.logTexts(r.ID))
			}
		}
	}()
	rtWait(f.t, 10*time.Second, "both lines and their offset to be stored", func() bool {
		runs, err := f.st.ListRuns(context.Background(), store.RunFilter{TaskID: taskID})
		if err != nil || len(runs) != 1 || runs[0].Status != domain.RunRunning {
			return false
		}
		run = runs[0]
		return len(f.logTexts(run.ID)) == 2 && run.StreamOffset > 0 && run.StreamOffset == f.streamSize(run.ID)
	})
	return run
}

func TestRestartReattachesAndLosesNoLine(t *testing.T) {
	f := newReattachFixture(t, time.Hour, time.Hour)
	e1, stop1 := f.engine(time.Now)
	task := f.create(e1)
	run := f.waitStoredBothLines(task.ID)
	assert.NotNil(t, run.DeadlineAt, "the run's deadline is stored")
	assert.NotEmpty(t, run.StartSHA)

	// The daemon exits. Its run, its task and the agent behind the shim all stay running.
	stop1()
	assert.Equal(t, domain.TaskRunning, f.task(task.ID).Status)
	assert.Equal(t, domain.RunRunning, f.run(task.ID).Status)
	st := f.shimStatus(run.ID)
	require.NoError(t, syscall.Kill(st.ShimPID, 0), "the shim outlived the daemon")
	require.NoError(t, syscall.Kill(st.AgentPID, 0), "the agent outlived the daemon")

	// The agent finishes while no daemon is connected.
	require.NoError(t, os.WriteFile(f.gate, nil, 0o644))
	rtWait(t, 15*time.Second, "the agent to exit", func() bool { return f.shimStatus(run.ID).Exited })

	// A new daemon on the same store and data directory takes the run over.
	_, _ = f.engine(time.Now)
	f.waitTask(task.ID, domain.TaskDone)

	final := f.run(task.ID)
	assert.Equal(t, domain.RunSucceeded, final.Status)
	// The offset moves past each line that produced a log entry; the closing result line produces
	// none, so it may be left to be parsed (and dropped) again.
	assert.Greater(t, final.StreamOffset, run.StreamOffset)
	assert.LessOrEqual(t, final.StreamOffset, f.streamSize(run.ID))
	assert.Equal(t, []string{"line one", "line two", "line three", "line four"}, f.logTexts(run.ID),
		"every agent line is in the log once, in order")
	f.waitShimGone(run.ID) // the new daemon acked the end, so the shim exits
}

func TestReattachCarriesTheStallClockOver(t *testing.T) {
	const stall = 2 * time.Second
	f := newReattachFixture(t, stall, time.Hour)
	e1, stop1 := f.engine(time.Now)
	task := f.create(e1)
	run := f.waitStoredBothLines(task.ID)
	last := run.LastOutputAt
	require.False(t, last.IsZero())
	stop1()

	// The new daemon's clock says it is half a second before the old silence would reach the stall
	// timeout. Had the clock restarted at reattach, the stall would be a full timeout away.
	clock := &fakeClock{t: last.Add(stall - 500*time.Millisecond)}
	_, _ = f.engine(clock.Now)
	time.Sleep(1500 * time.Millisecond) // several watchdog ticks
	require.Equal(t, domain.TaskRunning, f.task(task.ID).Status, "not stalled before last output + stall timeout")

	clock.set(last.Add(stall + 100*time.Millisecond))
	got := f.waitTask(task.ID, domain.TaskBlocked)
	assert.Contains(t, got.BlockedReason, "stalled: no output for 2s")
	assert.Equal(t, domain.RunStalled, f.run(task.ID).Status)
	assert.Equal(t, []string{"line one", "line two"}, f.logTexts(run.ID))
	// The stall killed the agent through the shim's signal call.
	agent := f.shimStatus(run.ID).AgentPID
	rtWait(t, 15*time.Second, "the agent to be killed", func() bool { return syscall.Kill(agent, 0) == syscall.ESRCH })
	f.waitShimGone(run.ID)
}

func TestReattachKeepsTheStoredRunDeadline(t *testing.T) {
	f := newReattachFixture(t, time.Hour, time.Hour)
	e1, stop1 := f.engine(time.Now)
	task := f.create(e1)
	run := f.waitStoredBothLines(task.ID)
	stop1()

	// The run is nearly out of time. A new full timeout (an hour) must not be granted.
	deadline := time.Now().Add(1500 * time.Millisecond)
	_, err := f.st.UpdateRun(context.Background(), run.ID, func(r *domain.Run) error {
		r.DeadlineAt = &deadline
		return nil
	})
	require.NoError(t, err)

	_, _ = f.engine(time.Now)
	got := f.waitTask(task.ID, domain.TaskBlocked)
	assert.Contains(t, got.BlockedReason, "timed out")
	assert.Equal(t, domain.RunTimedOut, f.run(task.ID).Status)
	f.waitShimGone(run.ID)
}

func TestRestartWithoutShimInterruptsAndResumes(t *testing.T) {
	f := newReattachFixture(t, time.Hour, time.Hour)
	e1, stop1 := f.engine(time.Now)
	task := f.create(e1)
	run := f.waitStoredBothLines(task.ID)
	stop1()

	// The shim is gone (killed, or the machine rebooted): the run is interrupted, not failed, and
	// the task is re-queued without using its only attempt (max_attempts is 1). The new run
	// resumes the session the agent reported.
	st := f.shimStatus(run.ID)
	require.NoError(t, syscall.Kill(-st.AgentPGID, syscall.SIGKILL))
	require.NoError(t, syscall.Kill(st.ShimPID, syscall.SIGKILL))
	rtWait(t, 15*time.Second, "shim to die", func() bool { return syscall.Kill(st.ShimPID, 0) == syscall.ESRCH })
	require.NoError(t, os.WriteFile(f.gate, nil, 0o644)) // the second run's agent does not wait

	_, _ = f.engine(time.Now)
	f.waitTask(task.ID, domain.TaskDone)
	runs, err := f.st.ListRuns(context.Background(), store.RunFilter{TaskID: task.ID})
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Equal(t, domain.RunInterrupted, runs[1].Status)
	assert.Equal(t, domain.RunSucceeded, runs[0].Status)
	assert.Equal(t, 1, f.task(task.ID).Attempts)
	f.waitShimGone(runs[0].ID)
}

func TestReattachStopsTheShimWhenItsTaskIsNoLongerRunning(t *testing.T) {
	f := newReattachFixture(t, time.Hour, time.Hour)
	e1, stop1 := f.engine(time.Now)
	task := f.create(e1)
	run := f.waitStoredBothLines(task.ID)
	stop1()

	// While onett was down the task was cancelled; its agent must not be left running unwatched.
	_, err := f.st.UpdateTask(context.Background(), task.ID, func(tk *domain.Task) error {
		tk.Status = domain.TaskCancelled
		return nil
	})
	require.NoError(t, err)
	st := f.shimStatus(run.ID)

	_, _ = f.engine(time.Now)
	f.waitShimGone(run.ID)
	rtWait(t, 15*time.Second, "agent to die", func() bool { return syscall.Kill(-st.AgentPGID, 0) == syscall.ESRCH })
	rtWait(t, 15*time.Second, "recover to settle the run", func() bool { return f.run(task.ID).Status != domain.RunRunning })
	// recover still has the conversation sweep to do after failing the run; let it finish so the
	// engine is not cancelled in the middle of it.
	time.Sleep(500 * time.Millisecond)
}
