package engine

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

func TestReviewRunDoesNotLoadConfigFromTheBranch(t *testing.T) {
	var mu sync.Mutex
	noProjectConfig := map[string]bool{}
	script := func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		mu.Lock()
		noProjectConfig[spec.Env["ONETT_TASK_ID"]] = spec.NoProjectConfig
		mu.Unlock()
		return hfake.DefaultScript(ctx, spec, emit)
	}
	env := rtNewEngine(t, rtOpts{script: script})
	parent := env.waitStatus(env.create(domain.Task{Body: "[fake:commit]"}).ID, domain.TaskReview)

	review := env.create(domain.Task{
		Kind: domain.KindReview, Role: "reviewer", ParentID: parent.ID, Title: "Review " + parent.Title,
		Branch: parent.Branch, PRNumber: parent.PRNumber,
	})
	env.waitStatus(review.ID, domain.TaskDone)

	mu.Lock()
	defer mu.Unlock()
	reviewFlag, ok := noProjectConfig[review.ID]
	assert.True(t, ok, "the review run was not started")
	assert.True(t, reviewFlag, "a review run must not load config from the branch")
	parentFlag, ok := noProjectConfig[parent.ID]
	assert.True(t, ok, "the worker run was not started")
	assert.False(t, parentFlag)
}
