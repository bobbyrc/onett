package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentLimitsDefaultOff(t *testing.T) {
	cfg, err := Parse([]byte("projects: {}\n"), "")
	require.NoError(t, err)
	assert.False(t, cfg.Chat.AgentMayApproveMerges)
	assert.False(t, cfg.Chat.AgentMayResume)
}

func TestAgentLimitsCanBeSet(t *testing.T) {
	cfg, err := Parse([]byte("chat:\n  agent_may_approve_merges: true\n  agent_may_resume: true\n"), "")
	require.NoError(t, err)
	assert.True(t, cfg.Chat.AgentMayApproveMerges)
	assert.True(t, cfg.Chat.AgentMayResume)
}

func TestAgentLimitsMustBeBooleans(t *testing.T) {
	_, err := Parse([]byte("chat:\n  agent_may_resume: sometimes\n"), "")
	require.Error(t, err)
}

func TestAgentLimitsAreInTheSchema(t *testing.T) {
	for _, key := range []string{"agent_may_approve_merges", "agent_may_resume"} {
		n := schemaAt(t, "chat", key)
		assert.Equal(t, "bool", n.Type, key)
	}
}
