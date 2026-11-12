package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

// swWaiterMarker in a task's body tells swWaiterScript that the task is the waiter.
const swWaiterMarker = "LEAVE-WORK-AND-WAIT"

// swOwnWork is what the waiter writes to its worktree on the first run.
const swOwnWork = "work of the first attempt\n"

// swWaiterScript is the agent of every task in the test. The waiter (its prompt holds
// swWaiterMarker) writes own.txt on its first run, leaves it uncommitted and reports that it
// depends on *dep. On its second run it stores what own.txt holds in secondRunSaw, or "MISSING"
// when the file is not in its worktree. Every other task finishes without changes.
func swWaiterScript(dep, secondRunSaw *atomic.Pointer[string]) hfake.Script {
	var waiterRuns atomic.Int32
	return func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		outbox := filepath.Join(spec.Workdir, domain.OutboxDir)
		if err := os.MkdirAll(outbox, 0o755); err != nil {
			return harness.Result{IsError: true}, err
		}
		body := `{"status":"no_changes","summary":"done"}`
		if strings.Contains(spec.Prompt, swWaiterMarker) {
			own := filepath.Join(spec.Workdir, "own.txt")
			if waiterRuns.Add(1) == 1 {
				if err := os.WriteFile(own, []byte(swOwnWork), 0o644); err != nil {
					return harness.Result{IsError: true}, err
				}
				body = `{"status":"blocked","summary":"need the dependency","blocked_reason":"merge ` +
					*dep.Load() + ` first","depends_on":["` + *dep.Load() + `"]}`
			} else {
				saw := "MISSING"
				if data, err := os.ReadFile(own); err == nil {
					saw = string(data)
				}
				secondRunSaw.Store(&saw)
			}
		}
		if err := os.WriteFile(filepath.Join(outbox, domain.ResultFile), []byte(body), 0o644); err != nil {
			return harness.Result{IsError: true}, err
		}
		return harness.Result{FinalText: "ok"}, nil
	}
}

// swWaitIdle waits until no run is registered. A run sweeps its project's leftover worktrees
// before it is unregistered, so after this the sweep of every ended run has finished.
func swWaitIdle(t *testing.T, env *rtEnv) {
	t.Helper()
	rtWait(t, 20*time.Second, "every run to be unregistered", func() bool {
		env.e.runsMu.Lock()
		defer env.e.runsMu.Unlock()
		return len(env.e.active) == 0
	})
}

func TestSweepKeepsTheWorkOfATaskWaitingOnADependency(t *testing.T) {
	var dep, secondRunSaw atomic.Pointer[string]
	env := rtNewEngine(t, rtOpts{script: swWaiterScript(&dep, &secondRunSaw)})
	// The dependency is in flight and never runs here: it only has to be unmerged, then merged.
	other := wtParent(env, domain.TaskReview, "")
	id := other.ID
	dep.Store(&id)

	waiter := env.create(domain.Task{Title: "Waiter", Body: swWaiterMarker})
	rtWait(t, 20*time.Second, "the waiter to wait on its dependency", func() bool {
		got := env.task(waiter.ID)
		return got.Status == domain.TaskQueued && len(got.WaitingOn) == 1
	})
	swWaitIdle(t, env)
	runs := env.runs(waiter.ID)
	require.Len(t, runs, 1)
	firstDir := runs[0].Workdir
	require.FileExists(t, filepath.Join(firstDir, "own.txt"), "the first attempt left its work uncommitted in its worktree")

	// Another task of the same project runs and ends. The end of its run sweeps the project's
	// leftover worktrees while the waiter is still waiting.
	bystander := env.create(domain.Task{Title: "Bystander", Body: "nothing to do"})
	env.waitStatus(bystander.ID, domain.TaskDone)
	swWaitIdle(t, env)
	assert.FileExists(t, filepath.Join(firstDir, "own.txt"), "the sweep keeps the worktree of a task that may run again")

	wtSetStatus(env, other.ID, domain.TaskMerged)
	rtWait(t, 20*time.Second, "the waiter to run again", func() bool { return secondRunSaw.Load() != nil })
	assert.Equal(t, swOwnWork, *secondRunSaw.Load(), "the second attempt starts with the work of the first")
	// Let the second run finish before cleanup, so it does not write into a directory being removed.
	rtWait(t, 20*time.Second, "the waiter's second run to end", func() bool {
		return env.task(waiter.ID).Status != domain.TaskRunning
	})
	swWaitIdle(t, env)
}

// TestSweepKeepsTheUncommittedWorkOfACancelledTask exposes a gap that is still open. A waiting
// task is cancelled when its dependency ends blocked, the sweep removes the worktree of a
// cancelled task, and a person may still retry that task. Closing the gap needs a way to ask
// internal/workspace whether a worktree holds uncommitted work.
func TestSweepKeepsTheUncommittedWorkOfACancelledTask(t *testing.T) {
	if os.Getenv("ONETT_RUN_EXPOSING") == "" {
		t.Skip("exposes: the sweep deletes the uncommitted work of a cancelled task, which a person " +
			"can still retry; run with ONETT_RUN_EXPOSING=1")
	}
	var dep, secondRunSaw atomic.Pointer[string]
	env := rtNewEngine(t, rtOpts{script: swWaiterScript(&dep, &secondRunSaw)})
	other := wtParent(env, domain.TaskReview, "")
	id := other.ID
	dep.Store(&id)

	waiter := env.create(domain.Task{Title: "Waiter", Body: swWaiterMarker})
	rtWait(t, 20*time.Second, "the waiter to wait on its dependency", func() bool {
		got := env.task(waiter.ID)
		return got.Status == domain.TaskQueued && len(got.WaitingOn) == 1
	})
	swWaitIdle(t, env)
	runs := env.runs(waiter.ID)
	require.Len(t, runs, 1)
	firstDir := runs[0].Workdir
	require.FileExists(t, filepath.Join(firstDir, "own.txt"), "the first attempt left its work uncommitted in its worktree")

	// The dependency ends blocked, so onett cancels the waiter without running it again.
	wtSetStatus(env, other.ID, domain.TaskBlocked)
	env.waitStatus(waiter.ID, domain.TaskCancelled)

	// Another task of the same project runs and ends, which sweeps the leftover worktrees.
	bystander := env.create(domain.Task{Title: "Bystander", Body: "nothing to do"})
	env.waitStatus(bystander.ID, domain.TaskDone)
	swWaitIdle(t, env)
	assert.FileExists(t, filepath.Join(firstDir, "own.txt"), "the sweep keeps a cancelled task's worktree while it holds uncommitted work")

	// A person retries the cancelled task.
	require.NoError(t, env.e.RetryTask(env.ctx, waiter.ID, ""))
	rtWait(t, 20*time.Second, "the waiter to run again", func() bool { return secondRunSaw.Load() != nil })
	assert.Equal(t, swOwnWork, *secondRunSaw.Load(), "the retried attempt starts with the work of the first")
	rtWait(t, 20*time.Second, "the waiter's second run to end", func() bool {
		return env.task(waiter.ID).Status != domain.TaskRunning
	})
	swWaitIdle(t, env)
}
