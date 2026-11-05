package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// chatFollowupDebounce is how long after the first settled task the orchestrator waits before it
// follows up, so several tasks that settle together share one turn. It is also the least time
// between two follow-up turns in one conversation.
const chatFollowupDebounce = 2 * time.Minute

// chatFollowupEvery is how often the follow-up loop looks for settled tasks.
const chatFollowupEvery = 15 * time.Second

// chatFollowupScan bounds how many of the newest tasks one scan reads.
const chatFollowupScan = 1000

// chatFollowups wakes the orchestrator in a conversation when tasks it delegated from there
// settle. The state is in memory on purpose: a restart forgets what was pending and starts from
// the tasks' current statuses, so nothing is reported twice and no migration is needed.
type chatFollowups struct {
	mu      sync.Mutex
	seeded  bool
	seen    map[string]domain.TaskStatus // last status seen, per chat-linked task
	pending map[string]*chatPending      // by conversation ID
	lastRun map[string]time.Time         // when the last follow-up turn started, by conversation
}

// chatPending is what one conversation has not heard about yet.
type chatPending struct {
	since time.Time                    // when the first item arrived
	tasks map[string]domain.TaskStatus // task ID → the status that settled it
}

func newChatFollowups() *chatFollowups {
	return &chatFollowups{
		seen:    map[string]domain.TaskStatus{},
		pending: map[string]*chatPending{},
		lastRun: map[string]time.Time{},
	}
}

// chatFollowupStatus reports whether a task reaching s is news for the owner.
func chatFollowupStatus(s domain.TaskStatus) bool {
	switch s {
	case domain.TaskDone, domain.TaskBlocked, domain.TaskAwaitingApproval, domain.TaskCancelled, domain.TaskFailed:
		return true
	}
	return false
}

// chatFollowupLoop runs until ctx ends.
func (e *Engine) chatFollowupLoop(ctx context.Context) {
	t := time.NewTicker(chatFollowupEvery)
	defer t.Stop()
	for {
		e.chatFollowupTick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// chatFollowupTick records newly settled chat-linked tasks and starts a turn for each
// conversation that is due.
func (e *Engine) chatFollowupTick(ctx context.Context) {
	tasks, err := e.d.Store.ListTasks(ctx, store.TaskFilter{Limit: chatFollowupScan})
	if err != nil {
		e.d.Logger.Error("chat followup: list tasks", "err", err)
		return
	}
	now := e.d.Now()
	f := e.followups
	f.mu.Lock()
	first := !f.seeded
	f.seeded = true
	byID := map[string]domain.Task{}
	for _, t := range tasks {
		convID := t.ChatConversationID()
		if convID == "" {
			continue
		}
		byID[t.ID] = t
		prev, known := f.seen[t.ID]
		f.seen[t.ID] = t.Status
		if first || (known && prev == t.Status) || !chatFollowupStatus(t.Status) {
			continue
		}
		p := f.pending[convID]
		if p == nil {
			p = &chatPending{since: now, tasks: map[string]domain.TaskStatus{}}
			f.pending[convID] = p
		}
		p.tasks[t.ID] = t.Status
	}
	var due []string
	for convID, p := range f.pending {
		if now.Sub(p.since) >= chatFollowupDebounce && now.Sub(f.lastRun[convID]) >= chatFollowupDebounce {
			due = append(due, convID)
		}
	}
	f.mu.Unlock()
	sort.Strings(due)
	for _, convID := range due {
		e.chatFollowupStart(ctx, convID, byID, now)
	}
}

// chatFollowupStart starts the follow-up turn for one conversation, or leaves its items pending
// when the conversation is busy or paused, or drops them when it is closed or gone.
func (e *Engine) chatFollowupStart(ctx context.Context, convID string, byID map[string]domain.Task, now time.Time) {
	f := e.followups
	drop := func() {
		f.mu.Lock()
		delete(f.pending, convID)
		f.mu.Unlock()
	}
	cur, err := e.d.Store.GetConversation(ctx, convID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			drop()
		} else {
			e.d.Logger.Error("chat followup: get conversation", "conversation", convID, "err", err)
		}
		return
	}
	// A goal's conversation is woken by the goals loop, not here.
	if cur.TaskID != "" || cur.GoalID != "" || cur.ClosedAt != nil {
		drop()
		return
	}
	if e.Paused() || (cur.Project != "" && e.projectPaused(cur.Project)) || cur.Busy {
		return
	}

	// Take the pending items only now that the turn can start; items that settle meanwhile
	// land in a fresh batch.
	f.mu.Lock()
	p := f.pending[convID]
	if p == nil {
		f.mu.Unlock()
		return
	}
	items := p.tasks
	f.mu.Unlock()
	prompt := chatFollowupPrompt(items, byID, e.Config())

	won := false
	conv, err := e.d.Store.UpdateConversation(ctx, convID, func(c *domain.Conversation) error {
		if !c.Busy && c.ClosedAt == nil {
			c.Busy, won = true, true
		}
		return nil
	})
	if err != nil || !won {
		return
	}
	msg, err := e.d.Store.AppendChatMessage(ctx, domain.ChatMessage{
		ConversationID: conv.ID, Role: "system", Text: prompt,
	})
	if err != nil {
		e.d.Logger.Error("chat followup: store prompt", "conversation", conv.ID, "err", err)
		e.releaseConversation(context.WithoutCancel(ctx), conv.ID)
		return
	}
	e.d.Bus.Emit(ctx, domain.EvChatMessage, conv.Project, "", "", "tasks settled; orchestrator follows up", msg)
	if err := e.startTurn(ctx, conv, []string{prompt}, nil); err != nil {
		return // shutting down; startTurn released the conversation
	}
	f.mu.Lock()
	delete(f.pending, convID)
	f.lastRun[convID] = now
	f.mu.Unlock()
}

// chatFollowupPrompt is the system message the follow-up turn is given.
//
// The title, blocked reason and summary are agent-written, so they are redacted and sit under
// the evidence note: data, not instructions.
func chatFollowupPrompt(items map[string]domain.TaskStatus, byID map[string]domain.Task, cfg *config.Config) string {
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	b.WriteString("[onett] Tasks you delegated from this conversation have settled. " + evidenceNote + "\n")
	for _, id := range ids {
		t := byID[id]
		clean := func(s string, n int) string {
			return firstRunes(redactSecrets(oneLineText(s), evidenceEnv(cfg.Projects[t.Project])), n)
		}
		fmt.Fprintf(&b, "\n- %s (task %s): now %s", clean(t.Title, 120), t.ID, items[id])
		if t.PRURL != "" {
			fmt.Fprintf(&b, "\n  PR: %s", t.PRURL)
		}
		if t.BlockedReason != "" {
			fmt.Fprintf(&b, "\n  Blocked reason: %s", clean(t.BlockedReason, 400))
		}
		if t.Summary != "" {
			fmt.Fprintf(&b, "\n  Agent summary: %s", clean(t.Summary, 600))
		}
	}
	b.WriteString("\n\nTell the owner in 1-3 short sentences what changed and what, if anything, needs them. " +
		"If a task is blocked and the fix is within what the owner already asked for, act on it " +
		"(retry with a note, or queue the fix). Do not start new work the owner did not ask for.")
	return b.String()
}

// oneLineText collapses all whitespace runs to single spaces.
func oneLineText(s string) string { return strings.Join(strings.Fields(s), " ") }
