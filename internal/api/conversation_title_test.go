package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestListConversationsBackfillsUntitledOnes(t *testing.T) {
	x := newHarness(t)
	ctx := context.Background()
	asked, err := x.st.CreateConversation(ctx, domain.Conversation{Title: domain.NewChatTitle})
	require.NoError(t, err)
	_, err = x.st.AppendChatMessage(ctx, domain.ChatMessage{ConversationID: asked.ID, Role: "user", Text: "plan the release"})
	require.NoError(t, err)
	empty, err := x.st.CreateConversation(ctx, domain.Conversation{Title: domain.NewChatTitle})
	require.NoError(t, err)
	named, err := x.st.CreateConversation(ctx, domain.Conversation{Title: "Operations"})
	require.NoError(t, err)

	rec := x.do(t, http.MethodGet, "/api/v1/conversations", "", testToken)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out []domain.Conversation
	decode(t, rec, &out)
	titles := map[string]string{}
	for _, c := range out {
		titles[c.ID] = c.Title
	}
	assert.Equal(t, "plan the release", titles[asked.ID])
	assert.Equal(t, domain.NewChatTitle, titles[empty.ID], "no message yet, nothing to title from")
	assert.Equal(t, "Operations", titles[named.ID])

	stored, err := x.st.GetConversation(ctx, asked.ID)
	require.NoError(t, err)
	assert.Equal(t, "plan the release", stored.Title, "the backfill is stored")
}
