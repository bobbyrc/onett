package engine

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/scm"
)

// These tests cover mergeTask's failure paths: every way it can stop before, during or after the
// merge, and what state that leaves the task and the pull request in.

// cpmKicks is how many deploy-watch kicks are waiting: one for each task that moved to merged.
func cpmKicks(env *lpEnv) int { return len(env.e.loopKickChan(loopDeployWatch)) }

// cpmApproval puts task in awaiting_approval with a pending merge approval for head.
func cpmApproval(t *testing.T, env *lpEnv, task domain.Task, head string) domain.Approval {
	t.Helper()
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.Status = domain.TaskAwaitingApproval })
	a, err := env.st.CreateApproval(env.ctx, domain.Approval{
		TaskID: task.ID, Project: "demo", Kind: "merge", Title: "Merge: " + task.Title,
		HeadSHA: head, Status: domain.ApprovalPending,
	})
	require.NoError(t, err)
	return a
}

// cpmFailWriteAfterMerge makes the first task write after a successful merge fail, once: the
// write that would have recorded the merge.
func cpmFailWriteAfterMerge(env *lpEnv) (*cpStore, *cpSCM) {
	cs := cpWrap(env.e)
	w := cpWrapSCM(env.e, env.fake)
	var failWrite atomic.Bool
	w.afterMerge = func() { failWrite.Store(true) }
	cs.failOn("UpdateTask", func(int) bool { return failWrite.CompareAndSwap(true, false) })
	return cs, w
}

// cpmFailReadAfterMerge makes the first PR read after a successful merge fail, once.
func cpmFailReadAfterMerge(env *lpEnv) *cpSCM {
	w := cpWrapSCM(env.e, env.fake)
	var failRead atomic.Bool
	w.afterMerge = func() { failRead.Store(true) }
	w.getPRErr = func() error {
		if failRead.CompareAndSwap(true, false) {
			return errors.New("HTTP 502: bad gateway")
		}
		return nil
	}
	return w
}

// A store read that fails before the merge stops mergeTask before it touches the PR.
func TestMergeTaskStoreReadFailureMergesNothing(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	cs := cpWrap(env.e)
	cs.failOn("GetTask", cpAlways)

	err := env.e.mergeTask(env.ctx, task.ID, env.head)
	require.ErrorIs(t, err, errCPInjected)
	assert.Empty(t, env.fake.Merged)
	assert.Equal(t, domain.TaskReview, env.task(task.ID).Status)
}

// A task whose project is no longer in the config is refused before any merge.
func TestMergeTaskUnknownProjectMergesNothing(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	ghost, err := env.st.CreateTask(env.ctx, domain.Task{
		Project: "ghost", Kind: domain.KindFeature, Role: "implementer", Title: "Project removed from config",
		Status: domain.TaskReview, Branch: lpBranch, PRNumber: task.PRNumber, MaxAttempts: 3, Source: "api",
	})
	require.NoError(t, err)

	err = env.e.mergeTask(env.ctx, ghost.ID, env.head)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown project")
	assert.Empty(t, env.fake.Merged)
	assert.Equal(t, domain.TaskReview, env.task(ghost.ID).Status)
}

// When no SCM client can be built for the project, nothing is merged.
func TestMergeTaskWithoutSCMMergesNothing(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	env.e.d.SCMFor = func(*config.Project) (scm.SCM, error) { return nil, errors.New("no gh login") }

	err := env.e.mergeTask(env.ctx, task.ID, env.head)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scm: no gh login")
	assert.Empty(t, env.fake.Merged)
	assert.Equal(t, domain.TaskReview, env.task(task.ID).Status)
}

// If the merged PR cannot be read back, the task is left for the next shepherd pass to finish.
func TestMergeTaskReadBackFailureIsFinishedByNextPass(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	w := cpmFailReadAfterMerge(env)

	require.NoError(t, env.e.mergeTask(env.ctx, task.ID, env.head))
	assert.Equal(t, []int{task.PRNumber}, env.fake.Merged)
	assert.Equal(t, []string{env.head}, w.heads(), "the merge is pinned to the head it was given")
	got := env.task(task.ID)
	assert.Equal(t, domain.TaskReview, got.Status, "the task is left alone when the merged PR cannot be read back")
	assert.Empty(t, got.MergeSHA)
	assert.Equal(t, 0, cpmKicks(env))

	// The next pass sees the PR merged.
	env.run(loopPRShepherd)
	got = env.task(task.ID)
	require.Equal(t, domain.TaskMerged, got.Status)
	assert.Equal(t, env.head, got.MergeSHA)
	assert.Len(t, env.fake.Merged, 1, "the PR is not merged a second time")
	assert.Len(t, w.heads(), 1)
	assert.Equal(t, 1, cpmKicks(env))

	err := env.e.mergeTask(env.ctx, task.ID, env.head)
	var notMergeable *notMergeableError
	require.ErrorAs(t, err, &notMergeable)
	assert.Len(t, env.fake.Merged, 1)
}

// An owner who stops the task while the merge call is in flight keeps that stop; the merge does
// not move the task to merged.
func TestMergeTaskDoesNotReviveTaskTheOwnerStoppedDuringMerge(t *testing.T) {
	for _, status := range []domain.TaskStatus{domain.TaskCancelled, domain.TaskBlocked} {
		t.Run(string(status), func(t *testing.T) {
			env, task := lpShepherdEnv(t, lpNotesFile)
			w := cpWrapSCM(env.e, env.fake)
			w.afterMerge = func() {
				lpSetTask(t, env, task.ID, func(x *domain.Task) {
					x.Status = status
					if status == domain.TaskBlocked {
						x.BlockedReason = "stopped by owner"
					}
				})
			}

			require.NoError(t, env.e.mergeTask(env.ctx, task.ID, env.head))
			got := env.task(task.ID)
			assert.Equal(t, status, got.Status, "a merge that finishes late must not move a stopped task to merged")
			assert.Empty(t, got.MergeSHA)
			assert.Len(t, env.fake.Merged, 1)
			assert.Equal(t, 0, cpmKicks(env))

			env.run(loopPRShepherd)
			assert.Equal(t, status, env.task(task.ID).Status)
			assert.Len(t, env.fake.Merged, 1)
			assert.Equal(t, 0, cpmKicks(env))
		})
	}
}

// If the write that records a merge fails, the next shepherd pass still moves the task to merged.
func TestMergeTaskStatusWriteFailureIsFinishedByNextPass(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	_, w := cpmFailWriteAfterMerge(env)

	err := env.e.mergeTask(env.ctx, task.ID, env.head)
	require.ErrorIs(t, err, errCPInjected)
	assert.Equal(t, []int{task.PRNumber}, env.fake.Merged)
	assert.Equal(t, []string{env.head}, w.heads())
	assert.Equal(t, domain.TaskReview, env.task(task.ID).Status)
	assert.Equal(t, 0, cpmKicks(env))

	env.run(loopPRShepherd)
	got := env.task(task.ID)
	require.Equal(t, domain.TaskMerged, got.Status)
	assert.Equal(t, env.head, got.MergeSHA)
	assert.Len(t, env.fake.Merged, 1, "the PR is not merged a second time")
	assert.Equal(t, 1, cpmKicks(env))
}

// An owner approval whose merge cannot read the PR back leaves the task for the next pass.
func TestDecideApprovalReadBackFailureIsFinishedByNextPass(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	a := cpmApproval(t, env, task, env.head)
	w := cpmFailReadAfterMerge(env)

	got, err := env.e.DecideApproval(env.ctx, a.ID, true, "")
	require.NoError(t, err)
	assert.Equal(t, domain.ApprovalApproved, got.Status)
	assert.Equal(t, []int{task.PRNumber}, env.fake.Merged)
	assert.Equal(t, []string{env.head}, w.heads(), "the merge is pinned to the approved head")
	assert.Equal(t, domain.TaskAwaitingApproval, env.task(task.ID).Status)

	env.run(loopPRShepherd)
	merged := env.task(task.ID)
	require.Equal(t, domain.TaskMerged, merged.Status)
	assert.Equal(t, env.head, merged.MergeSHA)
	assert.Len(t, env.fake.Merged, 1, "the PR is not merged a second time")
	assert.Len(t, w.heads(), 1)
	pending, err := env.st.ListApprovals(env.ctx, domain.ApprovalPending)
	require.NoError(t, err)
	assert.Empty(t, pending)
}

// The shepherd's auto-merge still ends merged when the write recording the merge fails once.
// Expected to fail against the current code: the task is blocked although its PR merged.
func TestLoopShepherdStatusWriteFailureAfterMergeStillEndsMerged(t *testing.T) {
	if os.Getenv("ONETT_RUN_EXPOSING") == "" {
		t.Skip(`exposes: a store write that fails right after a successful auto-merge makes the shepherd block the task as "merge failed" although its PR merged; nothing ever marks it merged`)
	}
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })
	cs, _ := cpmFailWriteAfterMerge(env)

	_ = env.e.RunLoop(env.ctx, loopPRShepherd, "demo") // the pass that merges; the write recording the merge fails once
	require.Equal(t, []int{task.PRNumber}, env.fake.Merged, "the PR merged")

	cs.heal()
	_ = env.e.RunLoop(env.ctx, loopPRShepherd, "demo") // a later pass, with the store working again
	got := env.task(task.ID)
	assert.Equal(t, domain.TaskMerged, got.Status, "the PR merged, so the task must end merged (blocked reason: %q)", got.BlockedReason)
	assert.Equal(t, env.head, got.MergeSHA)
	assert.Len(t, env.fake.Merged, 1)
}

// An owner's approval whose merge succeeds still ends merged when the write recording the merge
// fails once. Expected to fail against the current code: the task is blocked although its PR merged.
func TestDecideApprovalStatusWriteFailureAfterMergeStillEndsMerged(t *testing.T) {
	if os.Getenv("ONETT_RUN_EXPOSING") == "" {
		t.Skip(`exposes: a store write that fails right after an approved merge blocks the task as "merge failed after approval" although its PR merged; nothing ever marks it merged`)
	}
	env, task := lpShepherdEnv(t, lpNotesFile)
	a := cpmApproval(t, env, task, env.head)
	cs, w := cpmFailWriteAfterMerge(env)

	_, _ = env.e.DecideApproval(env.ctx, a.ID, true, "") // the owner approves; the write recording the merge fails once
	require.Equal(t, []int{task.PRNumber}, env.fake.Merged, "the PR merged")
	require.Equal(t, []string{env.head}, w.heads())

	cs.heal()
	_ = env.e.RunLoop(env.ctx, loopPRShepherd, "demo")
	got := env.task(task.ID)
	assert.Equal(t, domain.TaskMerged, got.Status, "the PR merged, so the task must end merged (blocked reason: %q)", got.BlockedReason)
	assert.Equal(t, env.head, got.MergeSHA)
	assert.Len(t, env.fake.Merged, 1)
}
