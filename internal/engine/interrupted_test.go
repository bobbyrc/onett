package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
)

// intSeed stores a task that was running, with one live-looking run that has no shim behind it,
// as a daemon that was killed mid-run leaves them.
func intSeed(t *testing.T, env *rtEnv, task domain.Task, sessionID string) (domain.Task, domain.Run) {
	t.Helper()
	if task.Project == "" {
		task.Project = "demo"
	}
	if task.Kind == "" {
		task.Kind = domain.KindFeature
	}
	if task.Role == "" {
		task.Role = "implementer"
	}
	if task.Title == "" {
		task.Title = "Interrupted work"
	}
	task.Status = domain.TaskRunning
	if task.MaxAttempts == 0 {
		task.MaxAttempts = 3
	}
	task, err := env.st.CreateTask(env.ctx, task)
	require.NoError(t, err)
	run, err := env.st.CreateRun(env.ctx, domain.Run{
		TaskID: task.ID, Project: task.Project, Role: task.Role, Harness: "fake",
		Status: domain.RunRunning, SessionID: sessionID, StartedAt: time.Now().Add(-time.Minute),
	})
	require.NoError(t, err)
	return task, run
}

func intWaitSettled(t *testing.T, env *rtEnv, taskID string) {
	t.Helper()
	fuWaitRuns(t, env, taskID, 2)
}

func TestInterruptedRunResumesItsSessionWithoutUsingAnAttempt(t *testing.T) {
	env, rec, _, _ := fuEnv(t)
	task, run := intSeed(t, env, domain.Task{Body: "[fake:commit]", Attempts: 1}, "sess-1")

	require.NoError(t, env.e.recover(env.ctx))
	intWaitSettled(t, env, task.ID)

	runs := env.runs(task.ID)
	require.Len(t, runs, 2)
	old := runs[1]
	assert.Equal(t, run.ID, old.ID)
	assert.Equal(t, domain.RunInterrupted, old.Status, "an interruption is not an agent failure")
	specs := rec.all()
	require.Len(t, specs, 1)
	assert.Equal(t, "sess-1", specs[0].ResumeSessionID)
	assert.Contains(t, specs[0].Prompt, interruptedNote)
	// The resumed prompt carries no task body, so the fake agent reports no changes.
	got := env.waitStatus(task.ID, domain.TaskDone)
	assert.Equal(t, 1, got.Attempts, "the resumed run took the attempt the interrupted one gave back")
}

func TestInterruptedRunOnTheLastAttemptIsResumedNotBlocked(t *testing.T) {
	env, rec, _, _ := fuEnv(t)
	task, _ := intSeed(t, env, domain.Task{Body: "[fake:commit]", Attempts: 3}, "sess-9")

	require.NoError(t, env.e.recover(env.ctx))
	got := env.waitStatus(task.ID, domain.TaskDone)

	assert.Equal(t, 3, got.Attempts)
	specs := rec.all()
	require.Len(t, specs, 1)
	assert.Equal(t, "sess-9", specs[0].ResumeSessionID)
}

func TestInterruptedRunWithoutSessionStartsFreshWithTheNote(t *testing.T) {
	env, rec, _, _ := fuEnv(t)
	task, _ := intSeed(t, env, domain.Task{Body: "[fake:commit]", Attempts: 1}, "")

	require.NoError(t, env.e.recover(env.ctx))
	env.waitStatus(task.ID, domain.TaskReview)

	specs := rec.all()
	require.Len(t, specs, 1)
	assert.Empty(t, specs[0].ResumeSessionID)
	assert.Contains(t, specs[0].Prompt, "[fake:commit]", "the task prompt is still there")
	assert.True(t, strings.HasSuffix(strings.TrimSpace(specs[0].Prompt), interruptedNote))
}

func TestInterruptedReviewIsResumedAndTheParentIsNotBlocked(t *testing.T) {
	env, rec, _, _ := fuEnv(t)
	parent, err := env.st.CreateTask(env.ctx, domain.Task{
		Project: "demo", Kind: domain.KindFeature, Role: "implementer", Title: "Parent",
		Status: domain.TaskReview, Branch: "onett/parent", MaxAttempts: 3,
	})
	require.NoError(t, err)
	review, _ := intSeed(t, env, domain.Task{
		Kind: domain.KindReview, Role: "reviewer", Title: "Review: Parent", ParentID: parent.ID,
		Branch: parent.Branch, Attempts: 3, SourceRef: "review:" + parent.ID + ":abc",
	}, "rev-sess")

	require.NoError(t, env.e.recover(env.ctx))
	rtWait(t, 20*time.Second, "the review to run again", func() bool { return len(rec.all()) == 1 })

	specs := rec.all()
	assert.Equal(t, "rev-sess", specs[0].ResumeSessionID)
	assert.Contains(t, specs[0].Prompt, interruptedNote)
	assert.NotEqual(t, domain.TaskBlocked, env.task(review.ID).Status)
	assert.NotEqual(t, domain.TaskBlocked, env.task(parent.ID).Status, "the review is not treated as ended without approving")
}

func TestRecoverStillFailsARunWhoseTaskIsNotRunning(t *testing.T) {
	env, _, _, _ := fuEnv(t)
	task, run := intSeed(t, env, domain.Task{Body: "x"}, "s")
	_, err := env.st.UpdateTask(env.ctx, task.ID, func(x *domain.Task) error {
		x.Status = domain.TaskCancelled
		return nil
	})
	require.NoError(t, err)

	require.NoError(t, env.e.recover(env.ctx))

	got, err := env.st.GetRun(env.ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.RunFailed, got.Status)
}

// A reattached run that owed setup (it did not pass before the agent started) runs it after the
// agent, as a run that never restarted does: here it still fails, so the task is blocked.
func TestReattachedRunRunsSetupItOwed(t *testing.T) {
	f := newReattachFixture(t, time.Hour, time.Hour)
	flag, logFile := filepath.Join(t.TempDir(), "flag"), filepath.Join(t.TempDir(), "setup.log")
	f.cfg.Projects["demo"].Setup = []config.Check{{Name: "prep", Run: "echo ran >> " + logFile + "; test -f " + flag}}
	e1, stop1 := f.engine(time.Now)
	task := f.create(e1)
	run := f.waitStoredBothLines(task.ID)
	assert.True(t, run.SetupOwed, "setup failed before the agent, so it is owed after it")
	stop1()

	require.NoError(t, os.WriteFile(f.gate, nil, 0o644))
	rtWait(t, 15*time.Second, "the agent to exit", func() bool { return f.shimStatus(run.ID).Exited })
	_, _ = f.engine(time.Now)

	got := f.waitTask(task.ID, domain.TaskBlocked)
	assert.Contains(t, got.BlockedReason, "setup failed: prep")
	lines, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(string(lines), "ran"), "once before the agent, once after it")
	f.waitShimGone(run.ID)
}
