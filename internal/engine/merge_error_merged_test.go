package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/scm"
)

// These tests cover a merge call that reports an error although the pull request did merge, the
// way gh does when its connection drops after GitHub accepted the merge.

// memSCM wraps the fake code host. Its MergePR merges and then reports mergeErr; getPRErr, when
// set, is asked before each GetPR and a non-nil error fails that read.
type memSCM struct {
	scm.SCM
	mergeErr error
	getPRErr func() error

	mu     sync.Mutex
	merges int // MergePR calls
	reads  int // GetPR calls made after the first MergePR call
}

// memWrap makes env's engine use a memSCM whose merge call always reports an error that is not
// one of the transient ones the shepherd retries.
func memWrap(env *lpEnv) *memSCM {
	s := &memSCM{SCM: env.fake, mergeErr: errors.New(`Post "https://api.github.com/graphql": context deadline exceeded`)}
	env.e.d.SCMFor = func(*config.Project) (scm.SCM, error) { return s, nil }
	return s
}

func (s *memSCM) MergePR(ctx context.Context, number int, headSHA string) error {
	s.mu.Lock()
	s.merges++
	s.mu.Unlock()
	if err := s.SCM.MergePR(ctx, number, headSHA); err != nil {
		return err
	}
	return s.mergeErr
}

func (s *memSCM) GetPR(ctx context.Context, number int) (scm.PR, error) {
	s.mu.Lock()
	if s.merges > 0 {
		s.reads++
	}
	s.mu.Unlock()
	if s.getPRErr != nil {
		if err := s.getPRErr(); err != nil {
			return scm.PR{}, err
		}
	}
	return s.SCM.GetPR(ctx, number)
}

// mergeCalls is how many times MergePR was called.
func (s *memSCM) mergeCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.merges
}

// readsAfterMerge is how many GetPR calls were made after the first MergePR call, this one
// included when called from getPRErr.
func (s *memSCM) readsAfterMerge() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

// memAssertMerged checks that the task ended merged at the judged head and that merge was called
// exactly once.
func memAssertMerged(t *testing.T, env *lpEnv, w *memSCM, id string) {
	t.Helper()
	got := env.task(id)
	assert.Equal(t, domain.TaskMerged, got.Status, "the PR merged, so the task must end merged (blocked reason: %q)", got.BlockedReason)
	assert.Equal(t, env.head, got.MergeSHA)
	assert.Len(t, env.fake.Merged, 1)
	assert.Equal(t, 1, w.mergeCalls(), "merge is never called a second time")
}

// mergeTask reads the PR before it believes a failed merge call: a PR that merged is recorded.
func TestMergeTaskRecordsAMergeWhoseCallReportedAnError(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	w := memWrap(env)

	require.NoError(t, env.e.mergeTask(env.ctx, task.ID, env.head))

	memAssertMerged(t, env, w, task.ID)
	assert.Equal(t, 1, cpmKicks(env), "the deploy watch is kicked for the merged task")
}

// The shepherd's auto-merge ends merged, not blocked, when the merge call reports an error for a
// merge that went through.
func TestLoopShepherdMergeErrorOnAMergedPRStillEndsMerged(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })
	w := memWrap(env)

	_ = env.e.RunLoop(env.ctx, loopPRShepherd, "demo") // the pass that merges
	_ = env.e.RunLoop(env.ctx, loopPRShepherd, "demo") // a later pass

	memAssertMerged(t, env, w, task.ID)
}

// An owner's approval ends merged, not blocked, when the merge call reports an error for a merge
// that went through.
func TestDecideApprovalMergeErrorOnAMergedPRStillEndsMerged(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	a := cpmApproval(t, env, task, env.head)
	w := memWrap(env)

	got, err := env.e.DecideApproval(env.ctx, a.ID, true, "")
	require.NoError(t, err)
	assert.Equal(t, domain.ApprovalApproved, got.Status)

	memAssertMerged(t, env, w, task.ID)
}

// When the merge call fails and the PR cannot be read either, nobody knows whether it merged. The
// shepherd leaves the task in review, and its next pass, which reads the PR first, records it.
func TestLoopShepherdMergeErrorWithUnreadablePRIsSettledByNextPass(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })
	w := memWrap(env)
	var down atomic.Bool
	down.Store(true)
	w.getPRErr = func() error {
		if down.Load() && w.mergeCalls() > 0 {
			return errors.New("dial tcp: connection refused")
		}
		return nil
	}

	_ = env.e.RunLoop(env.ctx, loopPRShepherd, "demo")
	got := env.task(task.ID)
	require.Equal(t, domain.TaskReview, got.Status, "an unknown merge outcome must not block (blocked reason: %q)", got.BlockedReason)

	down.Store(false)
	_ = env.e.RunLoop(env.ctx, loopPRShepherd, "demo")
	memAssertMerged(t, env, w, task.ID)
}

// The same for an owner's approval: with the merge outcome unknown the task is not blocked. It
// stays in awaiting_approval, and the shepherd's next pass records the merge.
func TestDecideApprovalMergeErrorWithUnreadablePRIsNotBlocked(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	a := cpmApproval(t, env, task, env.head)
	w := memWrap(env)
	var down atomic.Bool
	down.Store(true)
	w.getPRErr = func() error {
		if down.Load() && w.mergeCalls() > 0 {
			return errors.New("dial tcp: connection refused")
		}
		return nil
	}

	_, err := env.e.DecideApproval(env.ctx, a.ID, true, "")
	require.Error(t, err, "the owner is told the merge could not be confirmed")
	got := env.task(task.ID)
	require.Equal(t, domain.TaskAwaitingApproval, got.Status, "an unknown merge outcome must not block (blocked reason: %q)", got.BlockedReason)

	down.Store(false)
	_ = env.e.RunLoop(env.ctx, loopPRShepherd, "demo")
	memAssertMerged(t, env, w, task.ID)
	pending, err := env.st.ListApprovals(env.ctx, domain.ApprovalPending)
	require.NoError(t, err)
	assert.Empty(t, pending)
}

// If mergeTask could not read the PR but the shepherd's own re-read then shows it merged, the
// shepherd must not block the task as a refused merge.
func TestLoopShepherdDoesNotBlockWhenTheSecondReadShowsThePRMerged(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })
	w := memWrap(env)
	w.getPRErr = func() error {
		if w.readsAfterMerge() == 1 { // only the first read after the merge call fails
			return errors.New("dial tcp: connection refused")
		}
		return nil
	}

	_ = env.e.RunLoop(env.ctx, loopPRShepherd, "demo")
	got := env.task(task.ID)
	require.NotEqual(t, domain.TaskBlocked, got.Status, "the PR merged; blocked reason: %q", got.BlockedReason)

	_ = env.e.RunLoop(env.ctx, loopPRShepherd, "demo")
	memAssertMerged(t, env, w, task.ID)
}
