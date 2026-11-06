package engine

import (
	"testing"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestEffectiveSettingsFillsFromRoleDefaults(t *testing.T) {
	agent := config.Agent{Model: "opus", Effort: "high", PermissionMode: "plan"}

	got := effectiveSettings(agent, domain.ConversationSettings{})
	assert.Equal(t, domain.ConversationSettings{Model: "opus", Effort: "high", PermissionMode: "plan"}, got)

	got = effectiveSettings(agent, domain.ConversationSettings{Effort: "low"})
	assert.Equal(t, "low", got.Effort, "the conversation's own choice wins")
	assert.Equal(t, "plan", got.PermissionMode)
}
