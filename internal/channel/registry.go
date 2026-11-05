package channel

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
)

// Host is what a chat bot needs from the daemon: start conversations, send chat, decide
// approvals and watch events. EngineHost implements it over the engine; tests use fakes.
type Host interface {
	CreateConversation(ctx context.Context, title string) (domain.Conversation, error)
	SendChat(ctx context.Context, conversationID, text string) (domain.ChatMessage, error)
	DecideApproval(ctx context.Context, id string, approve bool, note string) (domain.Approval, error)
	Subscribe() (<-chan domain.Event, func())
}

// Bot is a running chat channel. Run blocks until ctx ends.
type Bot interface {
	Run(ctx context.Context) error
}

// Env is what a channel type needs from the process around it.
type Env struct {
	DataDir string
	Getenv  func(string) string
	Logger  *slog.Logger
}

// Factory validates a channel's options and resolves its token. The daemon calls it before
// anything starts, so a bad config or an empty token variable stops startup.
type Factory func(c config.Channel, env Env) (Starter, error)

// Starter builds the bot once the Host (engine, store, bus) exists.
type Starter func(host Host) (Bot, error)

// Registration describes one channel type.
type Registration struct {
	Factory Factory
	// SkipApprovalsForNotifier names a notifier type that must not also send approval requests
	// while this channel runs ("telegram" for telegram), so approvals reach the owner once.
	SkipApprovalsForNotifier string
}

var (
	mu    sync.RWMutex
	types = map[string]Registration{}
)

// Register adds a channel type. It panics on an empty or duplicate type, like notify.Register's
// callers expect: both are programming errors found at start-up.
func Register(typ string, r Registration) {
	if typ == "" {
		panic("channel: Register with empty type")
	}
	if r.Factory == nil {
		panic(fmt.Sprintf("channel: Register %q with nil Factory", typ))
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := types[typ]; dup {
		panic(fmt.Sprintf("channel: type %q registered twice", typ))
	}
	types[typ] = r
}

// Lookup returns the registration for a channel type.
func Lookup(typ string) (Registration, bool) {
	mu.RLock()
	defer mu.RUnlock()
	r, ok := types[typ]
	return r, ok
}

// Types lists the registered channel types, sorted.
func Types() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(types))
	for t := range types {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
