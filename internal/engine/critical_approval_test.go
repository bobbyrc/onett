package engine

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

// These tests cover requestMergeApproval's undo (backToReview): what happens to a task when the
// approval it was moved to awaiting_approval for could not be created.

// cpaEnv returns a supervised project whose task needs the owner's merge approval on the next
// shepherd pass, with a fault-injecting store installed.
func cpaEnv(t *testing.T) (*lpEnv, domain.Task, *cpStore) {
	t.Helper()
	env, task := lpShepherdEnvPolicy(t, map[string]string{"migrations/0001_init.sql": "create table t();\n"}, "supervised")
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })
	return env, task, cpWrap(env.e)
}

// cpaPending lists the pending approvals, read from the raw store.
func cpaPending(t *testing.T, env *lpEnv) []domain.Approval {
	t.Helper()
	out, err := env.st.ListApprovals(env.ctx, domain.ApprovalPending)
	require.NoError(t, err)
	return out
}

// A failed approval create must put the task back in review and must not leave a pending approval
// behind; the next healthy pass then asks for the approval again.
func TestApprovalCreateFailureReturnsTaskToReview(t *testing.T) {
	env, task, cs := cpaEnv(t)
	cs.failOn("CreateApproval", cpAlways)

	err := env.e.RunLoop(env.ctx, loopPRShepherd, "demo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create approval")
	assert.Equal(t, domain.TaskReview, env.task(task.ID).Status, "the task must not be left awaiting an approval nobody was asked for")
	assert.Empty(t, cpaPending(t, env))
	assert.Empty(t, env.fake.Merged)

	cs.heal()
	env.run(loopPRShepherd)

	assert.Equal(t, domain.TaskAwaitingApproval, env.task(task.ID).Status)
	pending := cpaPending(t, env)
	require.Len(t, pending, 1)
	assert.Equal(t, task.ID, pending[0].TaskID)
	assert.Equal(t, env.head, pending[0].HeadSHA)
	assert.Empty(t, env.fake.Merged)
}

// A failed approval list is handled the same way as a failed create: the task goes back to review.
func TestApprovalListFailureReturnsTaskToReview(t *testing.T) {
	env, task, cs := cpaEnv(t)
	cs.failOn("ListApprovals", cpAlways)

	err := env.e.RunLoop(env.ctx, loopPRShepherd, "demo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list approvals")
	assert.Equal(t, domain.TaskReview, env.task(task.ID).Status, "the task must not be left awaiting an approval nobody was asked for")
	assert.Empty(t, cpaPending(t, env))
	assert.Empty(t, env.fake.Merged)

	cs.heal()
	env.run(loopPRShepherd)

	assert.Equal(t, domain.TaskAwaitingApproval, env.task(task.ID).Status)
	pending := cpaPending(t, env)
	require.Len(t, pending, 1)
	assert.Equal(t, task.ID, pending[0].TaskID)
	assert.Equal(t, env.head, pending[0].HeadSHA)
	assert.Empty(t, env.fake.Merged)
}

// If the undo's own write also fails, the task stays awaiting approval with nothing pending; later
// passes must heal it rather than leave it stuck.
func TestApprovalUndoFailureIsHealedByLaterPasses(t *testing.T) {
	env, task, cs := cpaEnv(t)
	var undo atomic.Bool
	cs.failOn("CreateApproval", func(int) bool { undo.Store(true); return true })
	cs.failOn("UpdateTask", func(int) bool { return undo.CompareAndSwap(true, false) })

	err := env.e.RunLoop(env.ctx, loopPRShepherd, "demo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create approval")
	assert.Equal(t, domain.TaskAwaitingApproval, env.task(task.ID).Status, "the undo failed, so the task is still awaiting approval")
	assert.Empty(t, cpaPending(t, env))
	assert.Empty(t, env.fake.Merged)

	cs.heal()
	env.run(loopPRShepherd)

	assert.Equal(t, domain.TaskReview, env.task(task.ID).Status, "with no approval pending the next pass sends the task back to review")
	assert.Empty(t, cpaPending(t, env))

	env.run(loopPRShepherd)

	assert.Equal(t, domain.TaskAwaitingApproval, env.task(task.ID).Status)
	pending := cpaPending(t, env)
	require.Len(t, pending, 1)
	assert.Equal(t, env.head, pending[0].HeadSHA)
	assert.Empty(t, env.fake.Merged)
}

// If the owner stops the task while its approval is being created and the create then fails, the
// undo must leave the stopped task alone.
func TestApprovalFailureDoesNotReviveTaskTheOwnerStopped(t *testing.T) {
	for _, status := range []domain.TaskStatus{domain.TaskCancelled, domain.TaskBlocked} {
		t.Run(string(status), func(t *testing.T) {
			env, task, cs := cpaEnv(t)
			cs.failOn("CreateApproval", func(int) bool {
				lpSetTask(t, env, task.ID, func(x *domain.Task) {
					x.Status = status
					if status == domain.TaskBlocked {
						x.BlockedReason = "stopped by owner"
					}
				})
				return true
			})

			err := env.e.RunLoop(env.ctx, loopPRShepherd, "demo")
			require.Error(t, err)
			assert.Equal(t, status, env.task(task.ID).Status, "the undo must not put a stopped task back in review")
			assert.Empty(t, cpaPending(t, env))
			assert.Empty(t, env.fake.Merged)

			cs.heal()
			env.run(loopPRShepherd)

			assert.Equal(t, status, env.task(task.ID).Status, "the undo must not put a stopped task back in review")
			assert.Empty(t, cpaPending(t, env))
			assert.Empty(t, env.fake.Merged)
		})
	}
}
