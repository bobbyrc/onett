package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestFirstMessageTitlesANewChatConversation(t *testing.T) {
	r, e, _ := cqRig(t, llHarness())
	conv, err := r.st.CreateConversation(context.Background(), domain.Conversation{Title: domain.NewChatTitle, Project: "demo"})
	require.NoError(t, err)

	llSend(t, r, e, conv.ID, "why is the nightly deploy flaky?")
	got, err := r.st.GetConversation(context.Background(), conv.ID)
	require.NoError(t, err)
	assert.Equal(t, "why is the nightly deploy flaky?", got.Title)

	llSend(t, r, e, conv.ID, "second message")
	got, _ = r.st.GetConversation(context.Background(), conv.ID)
	assert.Equal(t, "why is the nightly deploy flaky?", got.Title, "a later message keeps the title")
}
