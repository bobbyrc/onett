package engine

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

// A task created with an old role name is stored and run under the current one.
func TestCreateTaskWithOldRoleNameStoresCurrentName(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	task := env.create(domain.Task{Title: "Add greeting", Role: "implementer", Body: "[fake:commit]"})
	assert.Equal(t, "senior-engineer", task.Role)
	env.waitStatus(task.ID, domain.TaskReview)
	runs := env.runs(task.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, "senior-engineer", runs[0].Role)
}

// A task row written before the rename still names "triager"; it dispatches to the tech lead.
func TestStoredTaskWithOldRoleStillDispatches(t *testing.T) {
	var mu sync.Mutex
	var roles []string
	env := rtNewEngine(t, rtOpts{script: func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		mu.Lock()
		roles = append(roles, spec.Env["ONETT_ROLE"])
		mu.Unlock()
		return hfake.DefaultScript(ctx, spec, emit)
	}})
	h, agent, err := env.e.harnessFor("demo", "triager")
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, "fake", agent.Harness, "the old name finds the tech lead the rig configured")

	old, err := env.st.CreateTask(context.Background(), domain.Task{
		Project: "demo", Kind: domain.KindQuestion, Role: "triager", Title: "Why?", Body: "Answer.",
		Status: domain.TaskQueued, MaxAttempts: 1, Source: "api",
	})
	require.NoError(t, err)
	env.e.wakeScheduler()
	env.waitStatus(old.ID, domain.TaskDone)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"tech-lead"}, roles)
}

func TestBuiltinPromptAcceptsOldName(t *testing.T) {
	e := &Engine{}
	old, err := e.BuiltinPrompt("implementer")
	require.NoError(t, err)
	cur, err := e.BuiltinPrompt("senior-engineer")
	require.NoError(t, err)
	assert.Equal(t, cur, old)
	for _, n := range []string{"product-designer", "visual-designer"} {
		p, err := e.BuiltinPrompt(n)
		require.NoError(t, err, n)
		assert.NotEmpty(t, p)
	}
}

func TestContextSectionListsOnlyExistingRepoPaths(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "DESIGN.md"), []byte("x"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "docs"), 0o755))

	got := contextSection([]string{"PRODUCT.md", "DESIGN.md", "docs/", "../outside", "/etc/passwd"}, dir)
	assert.Equal(t, "## Read these first\n\n- DESIGN.md\n- docs/\n\n", got)
	assert.Empty(t, contextSection([]string{"PRODUCT.md"}, dir), "no list when none exist")
	assert.Empty(t, contextSection(nil, dir))
}

// An agent's context list reaches the task prompt and its tools reach the harness spec.
func TestRunPassesContextAndToolsToHarness(t *testing.T) {
	var mu sync.Mutex
	var spec harness.Spec
	env := rtNewEngine(t, rtOpts{
		script: func(ctx context.Context, s harness.Spec, emit func(harness.Event)) (harness.Result, error) {
			mu.Lock()
			spec = s
			mu.Unlock()
			return hfake.DefaultScript(ctx, s, emit)
		},
		editConfig: func(c *config.Config) {
			a := c.Agents["senior-engineer"]
			a.Context = []string{"README.md", "MISSING.md"}
			a.Tools = []string{"Bash(go test:*)"}
			c.Agents["senior-engineer"] = a
		},
	})
	task := env.create(domain.Task{Title: "Add greeting", Body: "[fake:commit]"})
	env.waitStatus(task.ID, domain.TaskReview)
	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, spec.Prompt, "## Read these first\n\n- README.md\n")
	assert.NotContains(t, spec.Prompt, "MISSING.md")
	assert.Equal(t, []string{"Bash(go test:*)"}, spec.AllowedTools)
}
