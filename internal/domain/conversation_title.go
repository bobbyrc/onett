package domain

import "strings"

// NewChatTitle is the placeholder the web UI gives a chat before its first message. A
// conversation still carrying it (or no title) is untitled: the first owner message names it.
const NewChatTitle = "New chat"

// conversationTitleRunes is the longest title taken from a message.
const conversationTitleRunes = 60

// Untitled reports whether the conversation has no title of its own yet.
func (c Conversation) Untitled() bool {
	t := strings.TrimSpace(c.Title)
	return t == "" || t == NewChatTitle
}

// TitleFromMessage makes a conversation title from an owner message: its words on one line,
// cut at a word boundary with an ellipsis when long. attachment names the first attachment and
// is used when the message has no text. It returns "" when there is nothing to title from.
func TitleFromMessage(text, attachment string) string {
	s := strings.Join(strings.Fields(text), " ")
	if s == "" {
		s = strings.Join(strings.Fields(attachment), " ")
	}
	r := []rune(s)
	if len(r) <= conversationTitleRunes {
		return s
	}
	cut := string(r[:conversationTitleRunes])
	if i := strings.LastIndexByte(cut, ' '); i > conversationTitleRunes/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " .,;:!?-") + "…"
}
