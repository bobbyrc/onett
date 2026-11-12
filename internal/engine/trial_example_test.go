package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
)

// TestTrialExampleConfigLoads guards that examples/trial/onett.yaml parses and wires the scripted
// agent to both built-in roles with no credential.
func TestTrialExampleConfigLoads(t *testing.T) {
	cfg, err := config.Load("../../examples/trial/onett.yaml")
	require.NoError(t, err)

	script := cfg.Harness["script"]
	assert.Equal(t, "exec", script.Type)
	assert.Empty(t, script.InheritEnv, "the trial agent gets no credential")
	assert.Empty(t, script.Env, "the trial agent gets no credential")
	assert.Equal(t, "script", cfg.Agents["senior-engineer"].Harness)
	assert.Equal(t, "script", cfg.Agents["code-reviewer"].Harness)
	assert.Equal(t, "builtin:senior-engineer", cfg.Agents["senior-engineer"].Prompt)
	assert.True(t, cfg.Projects["trial"].Paused)
}

// TestTrialAgentWritesValidResults guards that examples/trial/agent.sh, run the way the exec
// harness runs it, writes a result the engine accepts for both the author and the reviewer role.
func TestTrialAgentWritesValidResults(t *testing.T) {
	script, err := filepath.Abs("../../examples/trial/agent.sh")
	require.NoError(t, err)

	t.Run("senior-engineer", func(t *testing.T) {
		dir := t.TempDir()
		out := runTrialAgent(t, script, dir, "senior-engineer")

		r, ok := parseResult(dir)
		require.True(t, ok, out)
		assert.Equal(t, domain.ResultCompleted, r.Status)
		assert.Equal(t, domain.RiskLow, r.Risk)
		assert.Nil(t, r.Review)
		assert.Equal(t, "Add a line to TRIAL.md", r.PRTitle)

		data, err := os.ReadFile(filepath.Join(dir, "TRIAL.md"))
		require.NoError(t, err)
		assert.Contains(t, string(data), "task-1")
	})

	t.Run("code-reviewer", func(t *testing.T) {
		dir := t.TempDir()
		out := runTrialAgent(t, script, dir, "code-reviewer")

		r, ok := parseResult(dir)
		require.True(t, ok, out)
		assert.Equal(t, domain.ResultCompleted, r.Status)
		assert.Equal(t, domain.RiskLow, r.Risk)
		require.NotNil(t, r.Review)
		assert.True(t, r.Review.Approve)
		assert.NoFileExists(t, filepath.Join(dir, "TRIAL.md"))
	})
}

// runTrialAgent runs the script in dir with only the variables the engine would give an agent,
// and returns its combined output.
func runTrialAgent(t *testing.T, script, dir, role string) string {
	t.Helper()
	cmd := exec.Command(script)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader("Do the task.\n")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"ONETT_OUTBOX=" + filepath.Join(dir, domain.OutboxDir),
		"ONETT_ROLE=" + role,
		"ONETT_TASK_ID=task-1",
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}
