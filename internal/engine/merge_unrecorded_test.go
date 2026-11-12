package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

// These tests cover what mergeTask's two callers do at the moment a PR has merged and the write
// that records it fails: neither may block the task, because the merge was not refused.

// The shepherd reports the failed write as a loop error and leaves the task in review.
func TestLoopShepherdReportsAnUnrecordedMergeAndDoesNotBlock(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })
	cpmFailWriteAfterMerge(env)

	err := env.e.RunLoop(env.ctx, loopPRShepherd, "demo")
	require.ErrorIs(t, err, errCPInjected, "the failed write is reported, not swallowed")
	var unrecorded *mergedUnrecordedError
	require.ErrorAs(t, err, &unrecorded)
	assert.Equal(t, []int{task.PRNumber}, env.fake.Merged)

	got := env.task(task.ID)
	assert.Equal(t, domain.TaskReview, got.Status, "the PR merged, so the task is not blocked")
	assert.Empty(t, got.BlockedReason)
	assert.Equal(t, 0, cpmKicks(env), "the deploy watch is kicked only once the merge is recorded")
}

// An owner's approval whose merge happened is reported as decided, and the task is not blocked.
func TestDecideApprovalWithAnUnrecordedMergeSucceedsAndDoesNotBlock(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	a := cpmApproval(t, env, task, env.head)
	cpmFailWriteAfterMerge(env)

	got, err := env.e.DecideApproval(env.ctx, a.ID, true, "")
	require.NoError(t, err, "the approval was recorded and the PR merged")
	assert.Equal(t, domain.ApprovalApproved, got.Status)
	assert.Equal(t, []int{task.PRNumber}, env.fake.Merged)

	after := env.task(task.ID)
	assert.Equal(t, domain.TaskAwaitingApproval, after.Status, "the PR merged, so the task is not blocked")
	assert.Empty(t, after.BlockedReason)
}
