package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A broken global agent is reported once, at the agent. A project that never mentions the
// agent must not repeat the problem under its own name: the owner would hunt in the project
// for a setting it does not have (found by the first-run browser journey).
func TestValidateReportsGlobalAgentProblemOnlyAtTheAgent(t *testing.T) {
	_, err := Parse([]byte(`
harnesses:
  fake:
    type: fake
agents:
  tester:
    harness: fake
  ghost:
    harness: nowhere
    prompt_text: hi
projects:
  app:
    repo: acme/app
`), "")
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "agents.tester.prompt: must not be empty")
	assert.Contains(t, msg, `agents.ghost.harness: unknown harness "nowhere"`)
	assert.NotContains(t, msg, "projects.app.agents.tester")
	assert.NotContains(t, msg, "projects.app.agents.ghost")
}

// A project override that breaks a global role is still reported, at the override.
func TestValidateReportsProjectOverrideProblems(t *testing.T) {
	_, err := Parse([]byte(`
harnesses:
  fake:
    type: fake
agents:
  tester:
    harness: fake
    prompt_text: hi
projects:
  app:
    repo: acme/app
    agents:
      tester:
        harness: nowhere
      extra:
        harness: fake
`), "")
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, `projects.app.agents.tester.harness: unknown harness "nowhere"`)
	assert.Contains(t, msg, "projects.app.agents.extra.prompt: must not be empty")
}
