package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestNewConversationSettingsNeedNoConversation(t *testing.T) {
	e, _, _ := settingsEngine(t)
	v, err := e.ConversationSettings(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, domain.ConversationSettings{}, v.Settings)
	assert.NotEmpty(t, v.Harness)
	assert.NotEmpty(t, v.Choices.Efforts)
}
