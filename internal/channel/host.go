// Package channel connects chat channels (see channel/telegram, channel/discord) to the daemon.
package channel

import (
	"context"

	"github.com/bobbyrc/onett/internal/channel/discord"
	"github.com/bobbyrc/onett/internal/channel/telegram"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/store"
)

// Controller is the part of the engine a channel drives.
type Controller interface {
	SendChat(ctx context.Context, conversationID, text string) (domain.ChatMessage, error)
	DecideApproval(ctx context.Context, id string, approve bool, note string) (domain.Approval, error)
}

// Host implements telegram.Host and discord.Host over the engine, the store and the event bus.
type Host struct {
	Controller Controller
	Store      store.Store
	Bus        events.Bus
}

var (
	_ telegram.Host = (*Host)(nil)
	_ discord.Host  = (*Host)(nil)
)

func (h *Host) CreateConversation(ctx context.Context, title string) (domain.Conversation, error) {
	return h.Store.CreateConversation(ctx, domain.Conversation{Title: title})
}

func (h *Host) SendChat(ctx context.Context, conversationID, text string) (domain.ChatMessage, error) {
	return h.Controller.SendChat(ctx, conversationID, text)
}

func (h *Host) DecideApproval(ctx context.Context, id string, approve bool, note string) (domain.Approval, error) {
	return h.Controller.DecideApproval(ctx, id, approve, note)
}

func (h *Host) Subscribe() (<-chan domain.Event, func()) { return h.Bus.Subscribe() }
