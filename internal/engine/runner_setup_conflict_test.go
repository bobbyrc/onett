package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

// A re-run on a tree with merge conflicts skips setup before the agent starts and tells the agent
// to resolve the conflicts and run the setup commands itself. Setup runs once more after the
// agent's turn.
func TestRunnerSkipsSetupOnConflictedTree(t *testing.T) {
	log := filepath.Join(t.TempDir(), "setup.log")
	prompts := make(chan string, 10)
	env := rtNewEngine(t, rtOpts{script: rtConflictScript(prompts), projectExtra: map[string]string{
		"demo": `    setup: [{name: deps, run: "echo ran >> ` + log + `"}]` + "\n",
	}})
	task := env.create(domain.Task{Title: "Add greeting", Body: "[fake:commit]"})
	got := env.waitStatus(task.ID, domain.TaskReview)
	require.NotZero(t, got.PRNumber)
	run := env.runs(task.ID)[0]

	clone := t.TempDir()
	rtGit(t, "", "clone", env.bare["demo"], clone)
	file := "fake-" + run.ID + ".txt"
	require.NoError(t, os.WriteFile(filepath.Join(clone, file), []byte("main side\n"), 0o644))
	rtGit(t, clone, "add", file)
	rtGit(t, clone, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "clash")
	rtGit(t, clone, "push", "origin", "HEAD:main")

	_, err := env.st.UpdateTask(env.ctx, task.ID, func(x *domain.Task) error {
		x.Status = domain.TaskQueued
		return nil
	})
	require.NoError(t, err)

	select {
	case prompt := <-prompts:
		assert.Contains(t, prompt, "- "+file)
		assert.Contains(t, prompt, "## Project setup was skipped")
		assert.Contains(t, prompt, "echo ran >> "+log)
	case <-time.After(20 * time.Second):
		t.Fatal("no run received the merge conflict prompt")
	}
	env.waitStatus(task.ID, domain.TaskBlocked)

	b, err := os.ReadFile(log)
	require.NoError(t, err)
	assert.Equal(t, len(env.runs(task.ID)), strings.Count(string(b), "ran"),
		"setup ran once per run (after the agent on conflicted runs), never before the agent")
}
