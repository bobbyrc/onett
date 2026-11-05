package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

// A re-run that pushes nothing leaves the PR head the reviewer approved, so the approval stays.
func TestRunnerRerunWithoutPushKeepsApproval(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	task := env.create(domain.Task{Title: "Add greeting", Body: "[fake:commit]"})
	got := env.waitStatus(task.ID, domain.TaskReview)
	pr, err := env.scm.GetPR(env.ctx, got.PRNumber)
	require.NoError(t, err)
	rtSetApproved(env, task.ID, pr.HeadSHA)
	// The re-run still commits locally, but the PR head the host reports stays put, as if
	// nothing was pushed.
	env.scm.HeadSHA = func(string) string { return pr.HeadSHA }

	again := env.rerun(task.ID)
	assert.Equal(t, pr.HeadSHA, again.ApprovedSHA)
}

// A re-run that moves the PR head drops the earlier approval.
func TestRunnerRerunWithNewHeadClearsApproval(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	task := env.create(domain.Task{Title: "Add greeting", Body: "[fake:commit]"})
	env.waitStatus(task.ID, domain.TaskReview)
	rtSetApproved(env, task.ID, "0000000000000000000000000000000000000000")

	again := env.rerun(task.ID)
	assert.Empty(t, again.ApprovedSHA)
}

func rtSetApproved(env *rtEnv, id, sha string) {
	env.t.Helper()
	_, err := env.st.UpdateTask(env.ctx, id, func(x *domain.Task) error {
		x.ApprovedSHA = sha
		return nil
	})
	require.NoError(env.t, err)
}
