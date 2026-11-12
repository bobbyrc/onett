package engine

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
)

// These tests cover resolveShippedTasks when one of its steps fails: a blocked task must stay
// blocked, untouched, so the next healthy release can try again, and one task's error must not
// stop the others.

// cpsFailed is the reason a failed release leaves on the task it shipped.
const cpsFailed = "release failed: health check failing; incident task inc-1"

// cpsEnv returns an engine over a real remote, and its project.
func cpsEnv(t *testing.T) (*lpEnv, lpRepo, *config.Project) {
	t.Helper()
	r := lpNewRepo(t)
	env := lpNewEngine(t, lpDoc(t, r.clone, "    deploy:\n      type: test\n      verify_for: 10m\n", ""))
	p, err := env.e.project("demo")
	require.NoError(t, err)
	return env, r, p
}

// cpsAssertBlocked checks that a task is still blocked with the reason its failed release left.
func cpsAssertBlocked(t *testing.T, env *lpEnv, id string) {
	t.Helper()
	got := env.task(id)
	assert.Equal(t, domain.TaskBlocked, got.Status)
	assert.Equal(t, cpsFailed, got.BlockedReason, "the reason is kept, so a later release can still resolve the task")
}

// cpsAssertDone checks that a task was resolved: done, with the block reason cleared.
func cpsAssertDone(t *testing.T, env *lpEnv, id string) {
	t.Helper()
	got := env.task(id)
	assert.Equal(t, domain.TaskDone, got.Status)
	assert.Empty(t, got.BlockedReason)
}

// cpsRevertChain adds n empty commits to main, each saying it reverts the one before it, starting
// with commit. It pushes them and returns the last one.
func cpsRevertChain(t *testing.T, r lpRepo, commit string, n int) string {
	t.Helper()
	prev := commit
	for i := 0; i < n; i++ {
		lpGit(t, r.seed, "commit", "--allow-empty", "-m", "Revert\n\nThis reverts commit "+prev+".")
		prev = lpRevParse(t, r.seed, "HEAD")
	}
	lpGit(t, r.seed, "push", "origin", "main")
	return prev
}

// A failed store listing must leave the task blocked, and the next healthy release resolves it.
func TestShippedCheckListFailureLeavesTaskForNextRelease(t *testing.T) {
	env, r, p := cpsEnv(t)
	shipped := lpShippedCommit(t, r, "shipped.txt")
	released := lpShippedCommit(t, r, "released.txt")
	task := lpBlockedTask(t, env, "Shipped despite failed release", shipped, cpsFailed)
	cs := cpWrap(env.e)
	cs.failOn("ListTasks", cpAlways)

	env.e.resolveShippedTasks(env.ctx, p, released)
	cpsAssertBlocked(t, env, task.ID)

	cs.heal()
	env.e.resolveShippedTasks(env.ctx, p, released)
	cpsAssertDone(t, env, task.ID)
}

// A failed fetch of the clone must leave the task blocked, and the next healthy release resolves it.
func TestShippedCheckFetchFailureLeavesTaskForNextRelease(t *testing.T) {
	env, r, p := cpsEnv(t)
	shipped := lpShippedCommit(t, r, "shipped.txt")
	released := lpShippedCommit(t, r, "released.txt")
	task := lpBlockedTask(t, env, "Shipped despite failed release", shipped, cpsFailed)

	require.NoError(t, os.Rename(r.remote, r.remote+".gone"))
	env.e.resolveShippedTasks(env.ctx, p, released)
	cpsAssertBlocked(t, env, task.ID)

	require.NoError(t, os.Rename(r.remote+".gone", r.remote))
	env.e.resolveShippedTasks(env.ctx, p, released)
	cpsAssertDone(t, env, task.ID)
}

// A commit git cannot resolve must not stop the other candidates from being checked.
func TestShippedCheckUnknownCommitDoesNotStopOtherTasks(t *testing.T) {
	env, r, p := cpsEnv(t)
	shipped := lpShippedCommit(t, r, "shipped.txt")
	released := lpShippedCommit(t, r, "released.txt")
	// Tasks are listed newest first. A shipped task sits on each side of the failing one, so one
	// of them is checked after the error whatever the order is.
	older := lpBlockedTask(t, env, "Shipped, created before", shipped, cpsFailed)
	// A merge commit the clone does not have: asking git about it is an error, not an answer.
	unknown := lpBlockedTask(t, env, "Commit git does not know", "0123456789abcdef0123456789abcdef01234567", cpsFailed)
	newer := lpBlockedTask(t, env, "Shipped, created after", shipped, cpsFailed)

	env.e.resolveShippedTasks(env.ctx, p, released)

	cpsAssertBlocked(t, env, unknown.ID)
	cpsAssertDone(t, env, older.ID)
	cpsAssertDone(t, env, newer.ID)
}

// An even chain of reverts leaves the change in effect, so four reverts resolve the task. Six is
// deeper than the workspace follows (5), which is an error, and an unknown answer must leave the
// task blocked.
func TestShippedCheckRevertChainTooDeepLeavesTaskBlocked(t *testing.T) {
	cases := []struct {
		name     string
		reverts  int
		wantDone bool
	}{
		{"four_reverts_back_in_effect", 4, true},
		{"six_reverts_too_deep_to_follow", 6, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env, r, p := cpsEnv(t)
			plain := lpShippedCommit(t, r, "plain.txt")
			target := lpShippedCommit(t, r, "target.txt")
			head := cpsRevertChain(t, r, target, c.reverts)
			// A shipped task on each side of the chained one: one of them is checked after it.
			older := lpBlockedTask(t, env, "Shipped, created before", plain, cpsFailed)
			chained := lpBlockedTask(t, env, "Reverted and reapplied", target, cpsFailed)
			newer := lpBlockedTask(t, env, "Shipped, created after", plain, cpsFailed)

			env.e.resolveShippedTasks(env.ctx, p, head)

			if c.wantDone {
				cpsAssertDone(t, env, chained.ID)
			} else {
				cpsAssertBlocked(t, env, chained.ID)
			}
			cpsAssertDone(t, env, older.ID)
			cpsAssertDone(t, env, newer.ID)
		})
	}
}

// A failed status write must leave the task blocked, and the next healthy release resolves it.
func TestShippedCheckStatusWriteFailureLeavesTaskForNextRelease(t *testing.T) {
	env, r, p := cpsEnv(t)
	shipped := lpShippedCommit(t, r, "shipped.txt")
	released := lpShippedCommit(t, r, "released.txt")
	task := lpBlockedTask(t, env, "Shipped despite failed release", shipped, cpsFailed)
	cs := cpWrap(env.e)
	cs.failOn("UpdateTask", cpAlways)

	env.e.resolveShippedTasks(env.ctx, p, released)
	cpsAssertBlocked(t, env, task.ID)

	cs.heal()
	env.e.resolveShippedTasks(env.ctx, p, released)
	cpsAssertDone(t, env, task.ID)
}

// A task the owner cancelled or requeued after it was listed must not be marked done.
func TestShippedCheckDoesNotFinishTaskTheOwnerMovedMeanwhile(t *testing.T) {
	for _, status := range []domain.TaskStatus{domain.TaskCancelled, domain.TaskQueued} {
		t.Run(string(status), func(t *testing.T) {
			env, r, p := cpsEnv(t)
			shipped := lpShippedCommit(t, r, "shipped.txt")
			released := lpShippedCommit(t, r, "released.txt")
			task := lpBlockedTask(t, env, "Shipped despite failed release", shipped, cpsFailed)
			cs := cpWrap(env.e)
			// The owner cancels or retries the task after it was listed as blocked.
			cs.afterCall("ListTasks", func() {
				lpSetTask(t, env, task.ID, func(x *domain.Task) {
					x.Status = status
					x.BlockedReason = ""
				})
			})

			env.e.resolveShippedTasks(env.ctx, p, released)

			assert.Equal(t, status, env.task(task.ID).Status, "a task the owner moved on must not be marked done")
		})
	}
}
