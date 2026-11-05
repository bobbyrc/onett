// Package channel connects chat channels (see channel/telegram, channel/discord) to the daemon.
package channel

import (
	"context"
	"errors"
	"io"

	"github.com/bobbyrc/onett/internal/attach"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/store"
)

// Controller is the part of the engine a channel drives.
type Controller interface {
	SendChat(ctx context.Context, conversationID, text string) (domain.ChatMessage, error)
	DecideApproval(ctx context.Context, id string, approve bool, note string) (domain.Approval, error)
	// SendChatWith is SendChat for a message that carries attachments saved with SaveAttachment.
	SendChatWith(ctx context.Context, conversationID, text string, attachmentIDs []string) (domain.ChatMessage, error)
}

// DefaultMaxAttachments is how many files one message may carry when EngineHost.MaxPerMessage
// is unset; it matches the chat.attachments.max_per_message default.
const DefaultMaxAttachments = 10

// EngineHost implements Host over the engine, the store and the event bus.
type EngineHost struct {
	Controller Controller
	Store      store.Store
	Bus        events.Bus
	// Attachments stores files that arrive over a channel. Nil turns them off.
	Attachments *attach.Manager
	// MaxPerMessage reports chat.attachments.max_per_message; nil means DefaultMaxAttachments.
	MaxPerMessage func() int
}

var _ Host = (*EngineHost)(nil)

func (h *EngineHost) CreateConversation(ctx context.Context, title string) (domain.Conversation, error) {
	return h.Store.CreateConversation(ctx, domain.Conversation{Title: title})
}

func (h *EngineHost) SendChat(ctx context.Context, conversationID, text string) (domain.ChatMessage, error) {
	return h.Controller.SendChat(ctx, conversationID, text)
}

func (h *EngineHost) SendChatWith(ctx context.Context, conversationID, text string, attachmentIDs []string) (domain.ChatMessage, error) {
	return h.Controller.SendChatWith(ctx, conversationID, text, attachmentIDs)
}

// SaveAttachment stores a file for the conversation. It returns attach.ErrTooLarge when r is
// over the limit.
func (h *EngineHost) SaveAttachment(ctx context.Context, conversationID, name string, r io.Reader) (domain.Attachment, error) {
	if h.Attachments == nil {
		return domain.Attachment{}, errors.New("attachments are turned off")
	}
	return h.Attachments.Save(ctx, conversationID, name, r)
}

// MaxAttachmentSize is the per-file limit in bytes, or 0 when attachments are off.
func (h *EngineHost) MaxAttachmentSize() int64 {
	if h.Attachments == nil {
		return 0
	}
	return h.Attachments.MaxSize()
}

// MaxAttachmentsPerMessage is how many files one chat message may carry.
func (h *EngineHost) MaxAttachmentsPerMessage() int {
	if h.MaxPerMessage != nil {
		if n := h.MaxPerMessage(); n > 0 {
			return n
		}
	}
	return DefaultMaxAttachments
}

func (h *EngineHost) DecideApproval(ctx context.Context, id string, approve bool, note string) (domain.Approval, error) {
	return h.Controller.DecideApproval(ctx, id, approve, note)
}

func (h *EngineHost) Subscribe() (<-chan domain.Event, func()) { return h.Bus.Subscribe() }
