package engine

import (
	"context"
	"strings"
	"unicode"

	"github.com/bobbyrc/onett/internal/domain"
)

// untitledChat is the placeholder title the web composer gives a conversation it creates before
// the first message exists. It counts as "no title": the first message replaces it.
const untitledChat = "New chat"

const chatTitleMax = 60

// needsChatTitle reports whether a conversation has no title worth keeping. Any other title was
// set on purpose (a goal, a rename) and is never overwritten.
func needsChatTitle(title string) bool {
	return title == "" || title == untitledChat
}

// deriveChatTitle makes a conversation title from its first message without calling a model: the
// first non-blank line, whitespace collapsed, cut to chatTitleMax runes at a word boundary, with
// trailing punctuation removed. It returns "" when the text has nothing to use.
func deriveChatTitle(text string) string {
	line := ""
	for _, l := range strings.Split(text, "\n") {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			line = l
			break
		}
	}
	r := []rune(line)
	if len(r) > chatTitleMax {
		cut := chatTitleMax
		if r[cut] != ' ' { // the limit falls inside a word: back up to the space before it
			for i := cut - 1; i > 0; i-- {
				if r[i] == ' ' {
					cut = i
					break
				}
			}
		}
		r = r[:cut]
	}
	return strings.TrimRightFunc(string(r), func(c rune) bool {
		return unicode.IsSpace(c) || unicode.IsPunct(c)
	})
}

// chatTitleFor derives a title from a first message, falling back to its first attachment's name.
func chatTitleFor(text string, atts []domain.Attachment) string {
	if t := deriveChatTitle(text); t != "" {
		return t
	}
	if len(atts) > 0 {
		return deriveChatTitle(atts[0].Name)
	}
	return ""
}

// backfillChatTitles gives orchestrator conversations that were left untitled (or "New chat")
// a title from their first owner message. It runs once at startup rather than on every read, so
// listing conversations stays a plain query; a conversation that already has a real title, or
// has no usable message yet, is left alone, and a second run changes nothing. It keeps each
// conversation's updated_at so the sidebar order does not change. It looks at the 10000 most
// recently updated conversations; older untitled ones stay as they are.
func (e *Engine) backfillChatTitles(ctx context.Context) {
	convs, err := e.d.Store.ListConversations(ctx, 10000)
	if err != nil {
		e.d.Logger.Warn("chat: backfill titles", "err", err)
		return
	}
	for _, c := range convs {
		if !needsChatTitle(c.Title) {
			continue
		}
		msgs, err := e.d.Store.ListChatMessages(ctx, c.ID)
		if err != nil {
			e.d.Logger.Warn("chat: backfill titles", "conversation", c.ID, "err", err)
			continue
		}
		title := ""
		for _, m := range msgs {
			if m.Role == "user" && m.Kind != "internal" {
				if title = chatTitleFor(m.Text, m.Attachments); title != "" {
					break
				}
			}
		}
		if title == "" {
			continue
		}
		if _, err := e.d.Store.UpdateConversationQuiet(ctx, c.ID, func(c *domain.Conversation) error {
			if needsChatTitle(c.Title) {
				c.Title = title
			}
			return nil
		}); err != nil {
			e.d.Logger.Warn("chat: backfill titles", "conversation", c.ID, "err", err)
		}
	}
}
