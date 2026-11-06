package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTitleFromMessage(t *testing.T) {
	long := strings.Repeat("investigate the flaky deploy ", 5)
	got := TitleFromMessage(long, "")
	assert.True(t, strings.HasSuffix(got, "…"))
	assert.LessOrEqual(t, len([]rune(got)), 61)
	assert.NotContains(t, got, "deplo…", "cut at a word boundary")

	assert.Equal(t, "fix the bug", TitleFromMessage("  fix the\n bug ", ""))
	assert.Equal(t, "shot.png", TitleFromMessage("", "shot.png"))
	assert.Equal(t, "", TitleFromMessage("  ", ""))
	assert.Equal(t, 61, len([]rune(TitleFromMessage(strings.Repeat("é", 70), ""))), "rune-safe")
}

func TestConversationUntitled(t *testing.T) {
	assert.True(t, Conversation{}.Untitled())
	assert.True(t, Conversation{Title: " New chat "}.Untitled())
	assert.False(t, Conversation{Title: "Operations"}.Untitled())
}
