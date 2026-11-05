package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/bobbyrc/onett/internal/domain"
)

// A merge that fails with a GitHub outage is tried again on later passes, and merges once the
// outage ends.
func TestLoopShepherdRetriesTransientMergeError(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })
	env.fake.MergeErr = errors.New("merge PR #1: GraphQL: Something went wrong while executing your query")

	env.run(loopPRShepherd)
	env.run(loopPRShepherd)
	if got := env.task(task.ID); got.Status != domain.TaskReview {
		t.Fatalf("task = %s %q, want review while retrying", got.Status, got.BlockedReason)
	}

	env.fake.MergeErr = nil
	env.run(loopPRShepherd)
	if len(env.fake.Merged) != 1 {
		t.Fatalf("merged = %v, want one merge after the outage ended", env.fake.Merged)
	}
}

// A transient error that never clears still blocks the task once the retries are used up.
func TestLoopShepherdBlocksAfterTransientMergeRetries(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })
	env.fake.MergeErr = errors.New("HTTP 502: Bad Gateway")

	for i := 0; i < maxMergeRetries+1; i++ {
		env.run(loopPRShepherd)
	}
	got := env.task(task.ID)
	if got.Status != domain.TaskBlocked || !strings.Contains(got.BlockedReason, "merge failed: ") {
		t.Fatalf("task = %s %q, want blocked after %d retries", got.Status, got.BlockedReason, maxMergeRetries)
	}
}
