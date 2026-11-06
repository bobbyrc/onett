package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestDeriveChatTitle(t *testing.T) {
	long := "Please investigate why the nightly deploy of the staging environment keeps failing"
	cases := []struct{ name, in, want string }{
		{"short", "Fix the login bug", "Fix the login bug"},
		{"trailing punctuation", "What is going on?!...", "What is going on"},
		{"long cut at word", long, "Please investigate why the nightly deploy of the staging"},
		{"multi-line uses first line", "\n\n  Plan the release \nthen do more\n", "Plan the release"},
		{"whitespace collapsed", "a \t  b  c", "a b c"},
		{"one long word", strings.Repeat("x", 90), strings.Repeat("x", 60)},
		{"emoji counted as runes", strings.Repeat("🚀", 70), strings.Repeat("🚀", 60)},
		{"blank", " \n\t ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := deriveChatTitle(c.in)
			assert.Equal(t, c.want, got)
			assert.LessOrEqual(t, len([]rune(got)), chatTitleMax)
		})
	}
}

func TestFirstMessageTitlesPlaceholderConversation(t *testing.T) {
	ctx := context.Background()
	cf := newCfRig(t)
	for name, tc := range map[string]struct{ before, want string }{
		"empty":       {"", "hello there"},
		"placeholder": {untitledChat, "hello there"},
		"explicit":    {"Operations", "Operations"},
	} {
		t.Run(name, func(t *testing.T) {
			conv, err := cf.r.st.CreateConversation(ctx, domain.Conversation{Project: "demo", Title: tc.before})
			require.NoError(t, err)
			_, err = cf.e.SendChat(ctx, conv.ID, "hello there.\nsecond line")
			require.NoError(t, err)
			got, err := cf.r.st.GetConversation(ctx, conv.ID)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Title)
		})
	}
}

func TestBackfillChatTitles(t *testing.T) {
	ctx := context.Background()
	cf := newCfRig(t)
	st := cf.r.st
	mk := func(title string, msgs ...domain.ChatMessage) string {
		c, err := st.CreateConversation(ctx, domain.Conversation{Title: title})
		require.NoError(t, err)
		for _, m := range msgs {
			m.ConversationID = c.ID
			_, err := st.AppendChatMessage(ctx, m)
			require.NoError(t, err)
		}
		return c.ID
	}
	user := func(s string) domain.ChatMessage { return domain.ChatMessage{Role: "user", Text: s} }
	placeholder := mk(untitledChat,
		domain.ChatMessage{Role: "user", Kind: "internal", Text: "check-in"},
		domain.ChatMessage{Role: "assistant", Text: "hi"}, user("Deploy the thing"), user("later"))
	blank := mk("", user("Another one"))
	named := mk("Operations", user("ignored"))
	empty := mk(untitledChat)

	cf.e.backfillChatTitles(ctx)
	cf.e.backfillChatTitles(ctx) // idempotent

	title := func(id string) string {
		c, err := st.GetConversation(ctx, id)
		require.NoError(t, err)
		return c.Title
	}
	assert.Equal(t, "Deploy the thing", title(placeholder))
	assert.Equal(t, "Another one", title(blank))
	assert.Equal(t, "Operations", title(named))
	assert.Equal(t, untitledChat, title(empty))
}
