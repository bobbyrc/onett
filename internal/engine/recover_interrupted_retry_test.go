package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

// These tests cover how recover tells an interrupted task from an ordinary orphan: by the status
// of the task's latest run, as stored.

const cprExhausted = "onett restarted during the last attempt and attempts are exhausted"

// A running task whose last run ended on its own is an ordinary orphan: the attempt counted, so
// on its last attempt the task is blocked.
func TestRecoverChargesTheAttemptWhenTheLastRunEndedOnItsOwn(t *testing.T) {
	env := cprEnv(t)
	task := cprTask(t, env, "On its last attempt", domain.TaskRunning, 3)
	cprRun(t, env, task, domain.RunFailed)

	require.NoError(t, env.e.recover(env.ctx))

	got := env.task(task.ID)
	assert.Equal(t, domain.TaskBlocked, got.Status)
	assert.Equal(t, cprExhausted, got.BlockedReason)
	assert.Equal(t, 3, got.Attempts)
}

// Only the latest run counts: an older interrupted run does not give back the attempt of a later
// run that ended on its own.
func TestRecoverChargesTheAttemptWhenOnlyAnOlderRunWasInterrupted(t *testing.T) {
	env := cprEnv(t)
	task := cprTask(t, env, "On its last attempt", domain.TaskRunning, 3)
	_, err := env.st.CreateRun(env.ctx, domain.Run{
		TaskID: task.ID, Project: "demo", Role: "implementer", Harness: "fake",
		Status: domain.RunInterrupted, StartedAt: time.Now().Add(-time.Hour),
	})
	require.NoError(t, err)
	cprRun(t, env, task, domain.RunFailed) // started a minute ago: the latest run

	require.NoError(t, env.e.recover(env.ctx))

	got := env.task(task.ID)
	assert.Equal(t, domain.TaskBlocked, got.Status)
	assert.Equal(t, cprExhausted, got.BlockedReason)
	assert.Equal(t, 3, got.Attempts)
}

// A task with no run at all (claimed just before a crash) is an ordinary orphan too.
func TestRecoverChargesTheAttemptOfATaskWithNoRun(t *testing.T) {
	env := cprEnv(t)
	task := cprTask(t, env, "On its last attempt", domain.TaskRunning, 3)

	require.NoError(t, env.e.recover(env.ctx))

	got := env.task(task.ID)
	assert.Equal(t, domain.TaskBlocked, got.Status)
	assert.Equal(t, cprExhausted, got.BlockedReason)
}

// If reading the task's runs fails, recover says so and leaves the task alone; the retry gives
// the attempt back.
func TestRecoverRunLookupFailureIsReturnedAndARetryGivesTheAttemptBack(t *testing.T) {
	env := cprEnv(t)
	task := cprTask(t, env, "On its last attempt", domain.TaskRunning, 3)
	cprRun(t, env, task, domain.RunRunning)
	cs := cpWrap(env.e)
	cs.failOn("ListRuns", cpCall(2)) // call 1 lists the live runs; call 2 reads this task's latest run

	require.ErrorIs(t, env.e.recover(env.ctx), errCPInjected)
	assert.Equal(t, domain.TaskRunning, env.task(task.ID).Status, "a failed lookup must not charge or re-queue")

	cs.heal()
	require.NoError(t, env.e.recover(env.ctx), "the next start finishes the recovery")
	got := env.task(task.ID)
	assert.Equal(t, domain.TaskQueued, got.Status, "blocked reason: %q", got.BlockedReason)
	assert.Equal(t, 2, got.Attempts)
}
