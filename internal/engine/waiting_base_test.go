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
)

// A task that left work behind and then reported depends_on has no pull request yet. When the
// task it waited for lands, its next run must start on a worktree that holds both its own earlier
// work and the code it waited for.
func TestReleasedWaiterThatLeftWorkBehindStartsOnTheCodeItWaitedFor(t *testing.T) {
	var dep atomic.Pointer[string]
	var calls atomic.Int32
	var sawOwn, sawDep atomic.Bool
	script := func(_ context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
		outbox := filepath.Join(spec.Workdir, domain.OutboxDir)
		if err := os.MkdirAll(outbox, 0o755); err != nil {
			return harness.Result{IsError: true}, err
		}
		body := `{"status":"no_changes","summary":"done"}`
		if calls.Add(1) == 1 {
			// Work left behind, uncommitted: agents never commit.
			if err := os.WriteFile(filepath.Join(spec.Workdir, "own.txt"), []byte("half done\n"), 0o644); err != nil {
				return harness.Result{IsError: true}, err
			}
			id := *dep.Load()
			body = `{"status":"blocked","summary":"need the other task","blocked_reason":"merge ` + id +
				` first","depends_on":["` + id + `"]}`
		} else {
			_, err := os.Stat(filepath.Join(spec.Workdir, "own.txt"))
			sawOwn.Store(err == nil)
			_, err = os.Stat(filepath.Join(spec.Workdir, "dep.txt"))
			sawDep.Store(err == nil)
		}
		if err := os.WriteFile(filepath.Join(outbox, domain.ResultFile), []byte(body), 0o644); err != nil {
			return harness.Result{IsError: true}, err
		}
		return harness.Result{FinalText: "ok"}, nil
	}
	env := rtNewEngine(t, rtOpts{script: script})
	other := wtParent(env, domain.TaskReview, "")
	id := other.ID
	dep.Store(&id)

	task := env.create(domain.Task{Body: "anything"})
	rtWait(t, 20*time.Second, "the task to wait on its dependency", func() bool {
		got := env.task(task.ID)
		return got.Status == domain.TaskQueued && len(got.WaitingOn) == 1
	})
	require.Zero(t, env.task(task.ID).PRNumber, "the waiter has no pull request yet")

	// The dependency merges: its code is on the default branch now.
	rtPushToMain(t, env.bare["demo"], map[string]string{"dep.txt": "from the dependency\n"})
	wtSetStatus(env, other.ID, domain.TaskMerged)

	// The second run reports no changes, but the branch holds the earlier work, so it is published.
	env.waitStatus(task.ID, domain.TaskReview)
	require.EqualValues(t, 2, calls.Load())
	assert.True(t, sawOwn.Load(), "the work the first run left behind is kept")
	assert.True(t, sawDep.Load(), "the worktree holds the code the task waited for")
}
