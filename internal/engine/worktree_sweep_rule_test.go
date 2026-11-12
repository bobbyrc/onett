package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// swTwoAttempts stores a task in status with two ended runs, an older and a newer one, gives
// each run a worktree and leaves an uncommitted file work.txt in each. It returns the two
// worktree directories, older first.
func swTwoAttempts(t *testing.T, env *lpEnv, status domain.TaskStatus) (string, string) {
	t.Helper()
	t0 := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	task := wsTask(t, env, status)
	older := wsRun(t, env, task.ID, t0, true)
	newer := wsRun(t, env, task.ID, t0.Add(time.Hour), true)
	olderDir := wsWorktree(t, env, older.ID)
	newerDir := wsWorktree(t, env, newer.ID)
	for _, dir := range []string{olderDir, newerDir} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "work.txt"), []byte("uncommitted\n"), 0o644))
	}
	return olderDir, newerDir
}

func TestSweepKeepsEveryWorktreeOfATaskThatMayRunAgain(t *testing.T) {
	env := wsNewEngine(t)
	p := env.e.Config().Projects["demo"]
	statuses := []domain.TaskStatus{
		domain.TaskQueued, domain.TaskRunning, domain.TaskReview,
		domain.TaskAwaitingApproval, domain.TaskBlocked, domain.TaskFailed,
	}
	type pair struct{ older, newer string }
	dirs := map[domain.TaskStatus]pair{}
	for _, s := range statuses {
		older, newer := swTwoAttempts(t, env, s)
		dirs[s] = pair{older, newer}
	}

	env.e.sweepWorktrees(env.ctx, p)

	for _, s := range statuses {
		assert.FileExists(t, filepath.Join(dirs[s].older, "work.txt"), "older attempt of a %s task keeps its uncommitted work", s)
		assert.FileExists(t, filepath.Join(dirs[s].newer, "work.txt"), "newest attempt of a %s task keeps its uncommitted work", s)
	}
}

func TestSweepRemovesTheWorktreesOfAFinishedTask(t *testing.T) {
	env := wsNewEngine(t)
	p := env.e.Config().Projects["demo"]
	statuses := []domain.TaskStatus{
		domain.TaskDone, domain.TaskMerged, domain.TaskDeploying, domain.TaskCancelled,
	}
	type pair struct{ older, newer string }
	dirs := map[domain.TaskStatus]pair{}
	for _, s := range statuses {
		older, newer := swTwoAttempts(t, env, s)
		dirs[s] = pair{older, newer}
	}

	env.e.sweepWorktrees(env.ctx, p)

	for _, s := range statuses {
		assert.False(t, wsExists(t, dirs[s].older), "older attempt of a %s task is removed", s)
		assert.False(t, wsExists(t, dirs[s].newer), "newest attempt of a %s task is removed", s)
	}
}
