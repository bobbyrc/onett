package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// These tests cover startup recovery when the store fails part-way through: recover must report
// the failure, must not bring back anything the owner stopped, and must leave the state such
// that the next start finishes the job.

// cprTask stores a task in status with attempts used out of 3.
func cprTask(t *testing.T, env *lpEnv, title string, status domain.TaskStatus, attempts int) domain.Task {
	t.Helper()
	task, err := env.st.CreateTask(env.ctx, domain.Task{
		Project: "demo", Kind: domain.KindFeature, Role: "implementer", Title: title,
		Status: status, Attempts: attempts, MaxAttempts: 3, Source: "api",
	})
	require.NoError(t, err)
	return task
}

// cprRun stores a run of task that still looks live, with no agent behind it, as a killed daemon
// leaves it.
func cprRun(t *testing.T, env *lpEnv, task domain.Task, status domain.RunStatus) domain.Run {
	t.Helper()
	run, err := env.st.CreateRun(env.ctx, domain.Run{
		TaskID: task.ID, Project: "demo", Role: "implementer", Harness: "fake",
		Status: status, SessionID: "sess-" + task.ID, StartedAt: time.Now().Add(-time.Minute),
	})
	require.NoError(t, err)
	return run
}

// cprFixture is what a killed daemon left behind.
type cprFixture struct {
	interrupted domain.Task // running, with a live-looking run
	orphan      domain.Task // running, claimed by the scheduler before its run row existed
	cancelled   domain.Task // cancelled by the owner, its run still marked running
	blocked     domain.Task // blocked, its run still marked starting
	conv        domain.Conversation
}

// cprSeed stores the fixture and turns the pause switch on.
func cprSeed(t *testing.T, env *lpEnv) cprFixture {
	t.Helper()
	var fx cprFixture
	fx.interrupted = cprTask(t, env, "Interrupted mid-run", domain.TaskRunning, 1)
	cprRun(t, env, fx.interrupted, domain.RunRunning)
	fx.orphan = cprTask(t, env, "Claimed, no run yet", domain.TaskRunning, 0)
	fx.cancelled = cprTask(t, env, "Cancelled by owner", domain.TaskCancelled, 1)
	cprRun(t, env, fx.cancelled, domain.RunRunning)
	fx.blocked = cprTask(t, env, "Blocked", domain.TaskBlocked, 1)
	lpSetTask(t, env, fx.blocked.ID, func(x *domain.Task) { x.BlockedReason = "stopped by owner" })
	cprRun(t, env, fx.blocked, domain.RunStarting)

	conv, err := env.st.CreateConversation(env.ctx, domain.Conversation{Title: "Chat", Busy: true})
	require.NoError(t, err)
	conv, err = env.st.GetConversation(env.ctx, conv.ID)
	require.NoError(t, err)
	require.True(t, conv.Busy, "the seeded conversation is busy")
	fx.conv = conv

	require.NoError(t, env.st.SaveLoopState(env.ctx, domain.LoopState{Name: controlLoop, Cursor: "paused"}))
	return fx
}

// cprAssertStopped checks that nothing the owner stopped came back.
func cprAssertStopped(t *testing.T, env *lpEnv, fx cprFixture) {
	t.Helper()
	assert.Equal(t, domain.TaskCancelled, env.task(fx.cancelled.ID).Status, "a cancelled task stays cancelled")
	got := env.task(fx.blocked.ID)
	assert.Equal(t, domain.TaskBlocked, got.Status, "a blocked task stays blocked")
	assert.Equal(t, "stopped by owner", got.BlockedReason)
}

// cprAssertRecovered checks the state a finished recovery leaves: no run or task still marked
// live, the orphaned tasks queued again, the conversation released, the pause switch restored.
func cprAssertRecovered(t *testing.T, env *lpEnv, fx cprFixture) {
	t.Helper()
	cprAssertStopped(t, env, fx)
	live, err := env.st.ListRuns(env.ctx, store.RunFilter{
		Statuses: []domain.RunStatus{domain.RunStarting, domain.RunRunning}, Limit: 100,
	})
	require.NoError(t, err)
	assert.Empty(t, live, "no run is still marked live")
	running, err := env.st.ListTasks(env.ctx, store.TaskFilter{
		Statuses: []domain.TaskStatus{domain.TaskRunning}, Limit: 100,
	})
	require.NoError(t, err)
	assert.Empty(t, running, "no task is still marked running")
	assert.Equal(t, domain.TaskQueued, env.task(fx.interrupted.ID).Status)
	assert.Equal(t, domain.TaskQueued, env.task(fx.orphan.ID).Status)
	conv, err := env.st.GetConversation(env.ctx, fx.conv.ID)
	require.NoError(t, err)
	assert.False(t, conv.Busy, "the busy conversation is released")
	assert.True(t, env.e.paused.Load(), "the pause switch is restored")
}

// cprEnv is an engine with no scheduler running, so a re-queued task stays queued.
func cprEnv(t *testing.T) *lpEnv {
	t.Helper()
	return lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
}

// A failure at any store call must surface as an error, leave the owner's stops alone, and
// let the next start finish the recovery.
func TestRecoverStoreFailureIsReturnedAndARetryFinishes(t *testing.T) {
	cases := []struct {
		method string
		call   int
	}{
		{"GetLoopState", 1}, {"ListLoopStates", 1}, {"ListRuns", 1},
		{"UpdateRun", 1}, {"UpdateRun", 2}, {"UpdateRun", 3},
		{"ListTasks", 1}, {"UpdateTask", 1}, {"UpdateTask", 2},
		{"ListConversations", 1}, {"UpdateConversation", 1},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s_call_%d", c.method, c.call), func(t *testing.T) {
			env := cprEnv(t)
			fx := cprSeed(t, env)
			cs := cpWrap(env.e)
			cs.failOn(c.method, cpCall(c.call))

			err := env.e.recover(env.ctx)
			require.ErrorIs(t, err, errCPInjected, "a start that could not finish recovering must say so")
			cprAssertStopped(t, env, fx)

			cs.heal()
			require.NoError(t, env.e.recover(env.ctx), "the next start finishes the recovery")
			cprAssertRecovered(t, env, fx)
		})
	}
}

// A daemon that cannot read its state must refuse to start, not schedule anything.
func TestRunRefusesToStartWhenRecoverFails(t *testing.T) {
	env := cprEnv(t)
	fx := cprSeed(t, env)
	cs := cpWrap(env.e)
	cs.failOn("GetLoopState", cpAlways)
	ctx, cancel := context.WithTimeout(env.ctx, 10*time.Second)
	defer cancel()

	err := env.e.Run(ctx)
	require.ErrorIs(t, err, errCPInjected)
	assert.Contains(t, err.Error(), "engine recover")
	assert.Equal(t, domain.TaskRunning, env.task(fx.orphan.ID).Status, "nothing was scheduled or re-queued")
	cprAssertStopped(t, env, fx)
}

// Control for the retry test below: a clean recovery gives the attempt of an interrupted run back.
func TestRecoverGivesBackTheAttemptOfAnInterruptedLastAttempt(t *testing.T) {
	env := cprEnv(t)
	task := cprTask(t, env, "On its last attempt", domain.TaskRunning, 3)
	cprRun(t, env, task, domain.RunRunning)

	require.NoError(t, env.e.recover(env.ctx))

	got := env.task(task.ID)
	assert.Equal(t, domain.TaskQueued, got.Status, "blocked reason: %q", got.BlockedReason)
	assert.Equal(t, 2, got.Attempts, "the restart, not the agent, ended the run: the attempt is given back")
}

// A recovery that failed after ending the run as interrupted must still give the attempt back on
// the retry.
func TestRecoverRetryStillGivesBackTheAttemptOfAnInterruptedRun(t *testing.T) {
	env := cprEnv(t)
	task := cprTask(t, env, "On its last attempt", domain.TaskRunning, 3)
	cprRun(t, env, task, domain.RunRunning)
	cs := cpWrap(env.e)
	cs.failOn("ListTasks", cpCall(1)) // the run is ended as interrupted, then the task sweep fails

	require.ErrorIs(t, env.e.recover(env.ctx), errCPInjected)
	cs.heal()
	require.NoError(t, env.e.recover(env.ctx), "the next start finishes the recovery")

	got := env.task(task.ID)
	assert.Equal(t, domain.TaskQueued, got.Status, "the task resumes as after a clean recovery (blocked reason: %q)", got.BlockedReason)
	assert.Equal(t, 2, got.Attempts, "the attempt is given back as after a clean recovery")
}

// The owner may cancel a task while recover is between listing it and re-queuing it; the
// re-queue must not undo that.
func TestRecoverDoesNotRequeueTaskTheOwnerCancelledMeanwhile(t *testing.T) {
	env := cprEnv(t)
	fx := cprSeed(t, env)
	cs := cpWrap(env.e)
	// The owner cancels both running tasks after recover has listed them and before it re-queues them.
	cs.afterCall("ListTasks", func() {
		for _, id := range []string{fx.interrupted.ID, fx.orphan.ID} {
			lpSetTask(t, env, id, func(x *domain.Task) { x.Status = domain.TaskCancelled })
		}
	})

	require.NoError(t, env.e.recover(env.ctx))

	assert.Equal(t, domain.TaskCancelled, env.task(fx.interrupted.ID).Status, "recover must not re-queue a task cancelled meanwhile")
	assert.Equal(t, domain.TaskCancelled, env.task(fx.orphan.ID).Status, "recover must not re-queue a task cancelled meanwhile")
	cprAssertStopped(t, env, fx)
}
