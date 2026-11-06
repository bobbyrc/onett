package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentEffortAndPermissionModeLoadAndProjectOverride(t *testing.T) {
	cfg, err := Parse([]byte(`
agents:
  triager:
    harness: claude
    prompt: builtin:triager
    effort: high
    permission_mode: plan
projects:
  app:
    repo: o/r
    agents:
      triager:
        effort: low
`), "")
	require.NoError(t, err)
	global := cfg.Agents["triager"]
	assert.Equal(t, "high", global.Effort)
	assert.Equal(t, "plan", global.PermissionMode)

	got := cfg.AgentsFor("app")["triager"]
	assert.Equal(t, "low", got.Effort, "a project override replaces the global value")
	assert.Equal(t, "plan", got.PermissionMode, "an unset override field keeps the global value")
}

func TestAgentEffortOnlyEntryIsNotJustDisabled(t *testing.T) {
	assert.False(t, onlyDisabled(Agent{Effort: "high"}))
	assert.False(t, onlyDisabled(Agent{PermissionMode: "plan"}))
}
