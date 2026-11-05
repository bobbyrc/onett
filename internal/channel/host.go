// Package channel connects chat channels (see channel/telegram, channel/discord) to the daemon.
package channel

import (
	"context"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/store"
)

// Controller is the part of the engine a channel drives.
type Controller interface {
	SendChat(ctx context.Context, conversationID, text string) (domain.ChatMessage, error)
	DecideApproval(ctx context.Context, id string, approve bool, note string) (domain.Approval, error)
}

// EngineHost implements Host over the engine, the store and the event bus.
type EngineHost struct {
	Controller Controller
	Store      store.Store
	Bus        events.Bus
}

var _ Host = (*EngineHost)(nil)

func (h *EngineHost) CreateConversation(ctx context.Context, title string) (domain.Conversation, error) {
	return h.Store.CreateConversation(ctx, domain.Conversation{Title: title})
}

func (h *EngineHost) SendChat(ctx context.Context, conversationID, text string) (domain.ChatMessage, error) {
	return h.Controller.SendChat(ctx, conversationID, text)
}

func (h *EngineHost) DecideApproval(ctx context.Context, id string, approve bool, note string) (domain.Approval, error) {
	return h.Controller.DecideApproval(ctx, id, approve, note)
}

func (h *EngineHost) Subscribe() (<-chan domain.Event, func()) { return h.Bus.Subscribe() }
