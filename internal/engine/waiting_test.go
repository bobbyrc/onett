package engine

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

// wtParent stores a task in the given status without running it, standing in for a task whose
// pull request is in review.
func wtParent(env *rtEnv, status domain.TaskStatus, goalID string) domain.Task {
	env.t.Helper()
	p, err := env.st.CreateTask(env.ctx, domain.Task{
		Project: "demo", Kind: domain.KindFeature, Title: "Parent", Role: "implementer",
		Status: status, Source: "api", GoalID: goalID, MaxAttempts: 2,
	})
	require.NoError(env.t, err)
	return p
}

func wtSetStatus(env *rtEnv, id string, status domain.TaskStatus) {
	env.t.Helper()
	_, err := env.st.UpdateTask(env.ctx, id, func(x *domain.Task) error {
		x.Status = status
		return nil
	})
	require.NoError(env.t, err)
	env.e.wakeScheduler()
}

func TestFollowupWaitsForParentToMerge(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	parent := wtParent(env, domain.TaskReview, "goal-1")
	child := env.create(domain.Task{ParentID: parent.ID, Source: "followup"})

	assert.Equal(t, []string{parent.ID}, child.WaitingOn)
	assert.Equal(t, "goal-1", child.GoalID, "a follow-up inherits its parent's goal")

	env.e.schedule(env.ctx)
	time.Sleep(100 * time.Millisecond)
	got := env.task(child.ID)
	assert.Equal(t, domain.TaskQueued, got.Status, "not claimed while the parent is unmerged")
	assert.Zero(t, got.Attempts)

	wtSetStatus(env, parent.ID, domain.TaskMerged)
	env.waitStatus(child.ID, domain.TaskReview)
	got = env.task(child.ID)
	assert.Empty(t, got.WaitingOn)
	assert.Contains(t, got.Body, "Dependencies landed")
}

func TestFollowupStartsWhenParentDoneWithoutPR(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	parent := wtParent(env, domain.TaskDone, "")
	child := env.create(domain.Task{ParentID: parent.ID, Source: "followup"})
	assert.Empty(t, child.WaitingOn, "a landed parent holds nothing back")
	env.waitStatus(child.ID, domain.TaskReview)
}

func TestFollowupCancelledWhenParentCancelled(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	parent := wtParent(env, domain.TaskReview, "")
	child := env.create(domain.Task{ParentID: parent.ID, Source: "followup"})

	wtSetStatus(env, parent.ID, domain.TaskCancelled)
	got := env.waitStatus(child.ID, domain.TaskCancelled)
	assert.Contains(t, got.BlockedReason, parent.ID)
	assert.Empty(t, got.WaitingOn)
	assert.Zero(t, got.Attempts, "it never ran")
}

func TestReviewTaskDoesNotWaitForItsParent(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	parent := wtParent(env, domain.TaskReview, "")
	review, err := env.e.createTask(env.ctx, domain.Task{
		Project: "demo", Kind: domain.KindReview, Role: "reviewer", ParentID: parent.ID, Title: "Review",
	})
	require.NoError(t, err)
	assert.Empty(t, review.WaitingOn)
}

// wtDependsScript blocks the first run naming dep as a dependency, then completes without changes.
func wtDependsScript(dep string) hfake.Script {
	var calls atomic.Int32
	return func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		dir := filepath.Join(spec.Workdir, domain.OutboxDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return harness.Result{IsError: true}, err
		}
		body := `{"status":"no_changes","summary":"done"}`
		if calls.Add(1) == 1 {
			body = `{"status":"blocked","summary":"need the parent","blocked_reason":"merge ` + dep +
				` first","depends_on":["` + dep + `","no-such-task"]}`
		}
		if err := os.WriteFile(filepath.Join(dir, domain.ResultFile), []byte(body), 0o644); err != nil {
			return harness.Result{IsError: true}, err
		}
		return harness.Result{FinalText: "ok"}, nil
	}
}

func TestBlockedWithDependsOnWaitsInsteadOfBlocking(t *testing.T) {
	// The script needs the dependency's id, so build the engine first and set it afterwards.
	var dep atomic.Pointer[string]
	env := rtNewEngine(t, rtOpts{script: lazyDependsScript(&dep)})
	other := wtParent(env, domain.TaskReview, "")
	id := other.ID
	dep.Store(&id)

	task := env.create(domain.Task{Body: "anything"})
	rtWait(t, 20*time.Second, "task to wait on its dependency", func() bool {
		got := env.task(task.ID)
		return got.Status == domain.TaskQueued && len(got.WaitingOn) == 1
	})
	got := env.task(task.ID)
	assert.Equal(t, []string{other.ID}, got.WaitingOn, "unknown ids are dropped")
	assert.Empty(t, got.BlockedReason)
	assert.Zero(t, got.Attempts, "waiting does not use up an attempt")

	wtSetStatus(env, other.ID, domain.TaskMerged)
	env.waitStatus(task.ID, domain.TaskDone)
	assert.Contains(t, env.task(task.ID).Body, "Dependencies landed")
}

func TestBlockedWithLandedDependsOnStillBlocks(t *testing.T) {
	var dep atomic.Pointer[string]
	env := rtNewEngine(t, rtOpts{script: lazyDependsScript(&dep)})
	other := wtParent(env, domain.TaskMerged, "")
	id := other.ID
	dep.Store(&id)

	task := env.create(domain.Task{Body: "anything"})
	got := env.waitStatus(task.ID, domain.TaskBlocked)
	assert.Contains(t, got.BlockedReason, "agent needs a human")
}

func lazyDependsScript(dep *atomic.Pointer[string]) hfake.Script {
	var inner atomic.Pointer[hfake.Script]
	return func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		if inner.Load() == nil {
			s := wtDependsScript(*dep.Load())
			inner.CompareAndSwap(nil, &s)
		}
		return (*inner.Load())(ctx, spec, emit)
	}
}
