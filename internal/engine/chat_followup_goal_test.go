package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

// A goal's conversation is woken by the goals loop, so the chat follow-up leaves it alone.
func TestChatFollowupSkipsGoalConversations(t *testing.T) {
	cf := newCfRig(t)
	cf.settle(domain.TaskDone)
	_, err := cf.r.st.UpdateConversation(context.Background(), cf.conv, func(c *domain.Conversation) error {
		c.GoalID = "goal-1"
		return nil
	})
	require.NoError(t, err)

	cf.advance(chatFollowupDebounce)
	cf.tick()
	assert.Equal(t, 0, cf.promptCount())
	_, still := cf.e.followups.pending[cf.conv]
	assert.False(t, still)
}
