package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

// bcGiveUp leaves a running task with its attempts used up, then gives up on it with why and
// detail, so requeueOrBlock blocks it with a cause.
func bcGiveUp(t *testing.T, env *lpEnv, why, detail string) domain.Task {
	t.Helper()
	task := lpRetroTask(t, env, domain.Task{Title: "Ship it", Status: domain.TaskRunning, Attempts: 3, MaxAttempts: 3})
	env.e.requeueOrBlock(env.ctx, task.ID, why, detail)
	return task
}

func TestGivingUpRecordsTheCause(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	t.Setenv("BC_DEPLOY_TOKEN", "bc-secret-value-123")
	detail := "git fetch origin: auth failed for bc-secret-value-123\nremote: access denied"
	task := bcGiveUp(t, env, "workspace setup failed", detail)

	got := env.task(task.ID)
	assert.Equal(t, domain.TaskBlocked, got.Status)
	assert.Equal(t, "workspace setup failed (gave up after 3 attempts)", got.BlockedReason)
	assert.Equal(t, "workspace setup failed", got.BlockedCause)
	assert.Contains(t, got.BlockedDetail, "remote: access denied")
	assert.Contains(t, got.BlockedDetail, "\n", "the detail keeps its line break")
	assert.NotContains(t, got.BlockedDetail, "bc-secret-value-123")
	require.NotNil(t, got.BlockedAt)
	assert.True(t, got.BlockedAt.Equal(env.now))
}

func TestPlainBlockRecordsNoCause(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	task := lpRetroTask(t, env, domain.Task{Title: "Ask", Status: domain.TaskRunning})
	env.e.blockTask(env.ctx, task.ID, "agent needs a human: x")

	got := env.task(task.ID)
	assert.Equal(t, domain.TaskBlocked, got.Status)
	assert.Equal(t, "agent needs a human: x", got.BlockedReason)
	assert.Empty(t, got.BlockedCause)
	assert.Empty(t, got.BlockedDetail)
	require.NotNil(t, got.BlockedAt)
}

func TestBlockedDetailKeepsItsEnd(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	detail := strings.Repeat("x", 5000-len("THE-END")) + "THE-END"
	task := bcGiveUp(t, env, "workspace setup failed", detail)

	got := []rune(env.task(task.ID).BlockedDetail)
	assert.Len(t, got, 2000)
	assert.True(t, strings.HasPrefix(string(got), "…"))
	assert.True(t, strings.HasSuffix(string(got), "THE-END"))
}

func TestRetryClearsTheCause(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	task := bcGiveUp(t, env, "workspace setup failed", "fetch failed")
	require.NotEmpty(t, env.task(task.ID).BlockedCause)

	require.NoError(t, env.e.RetryTask(env.ctx, task.ID, ""))

	got := env.task(task.ID)
	assert.Equal(t, domain.TaskQueued, got.Status)
	assert.Empty(t, got.BlockedCause)
	assert.Empty(t, got.BlockedDetail)
	assert.Nil(t, got.BlockedAt)
}

func TestReblockReplacesTheCause(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	task := bcGiveUp(t, env, "workspace setup failed", "fetch failed")
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.Status = domain.TaskRunning })

	env.e.blockTask(env.ctx, task.ID, "agent needs a human: x")

	got := env.task(task.ID)
	assert.Equal(t, "agent needs a human: x", got.BlockedReason)
	assert.Empty(t, got.BlockedCause)
	assert.Empty(t, got.BlockedDetail)
}
