package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
)

// operationsTitle is the title the Operations conversation is created with. The owner may rename
// it; the conversation is found by its Kind.
const operationsTitle = "Operations"

// operationsWaker is the default HeartbeatWaker: it posts the digest to the Operations
// conversation as an internal message and runs an orchestrator turn there, the same way a chat
// follow-up does.
type operationsWaker struct{ e *Engine }

// operationsConversation returns the Operations conversation, creating it on first use. Its
// project is the self project, or else the first configured one; the digest names the project of
// each item, so this only sets the default for delegated work.
func (e *Engine) operationsConversation(ctx context.Context) (domain.Conversation, error) {
	e.opsMu.Lock()
	defer e.opsMu.Unlock()
	convs, err := e.d.Store.ListConversations(ctx, 10000)
	if err != nil {
		return domain.Conversation{}, err
	}
	for _, c := range convs {
		if c.Kind == domain.ConversationOperations {
			return c, nil
		}
	}
	cfg := e.Config()
	project := cfg.SelfProject
	if _, ok := cfg.Projects[project]; !ok {
		project = ""
		if names := cfg.ProjectNames(); len(names) > 0 {
			project = names[0]
		}
	}
	return e.d.Store.CreateConversation(ctx, domain.Conversation{
		Title: operationsTitle, Project: project, Kind: domain.ConversationOperations,
	})
}

// WakeHeartbeat posts the digest and starts a turn, or queues the digest behind the turn that is
// running. An error leaves the heartbeat loop's state unsaved, so it tries again next tick.
func (w operationsWaker) WakeHeartbeat(ctx context.Context, d HeartbeatDigest) error {
	e := w.e
	conv, err := e.operationsConversation(ctx)
	if err != nil {
		return fmt.Errorf("operations conversation: %w", err)
	}
	prompt := heartbeatPrompt(d, e.Config())

	won := false
	conv, err = e.d.Store.UpdateConversation(ctx, conv.ID, func(c *domain.Conversation) error {
		if !c.Busy {
			c.Busy, won = true, true
		}
		return nil
	})
	if err != nil {
		return err
	}
	msg := domain.ChatMessage{ConversationID: conv.ID, Role: "system", Kind: domain.ChatKindInternal, Text: prompt}
	if !won {
		// A turn is running: the digest waits in the queue and goes out when that turn ends.
		msg.Queued = true
		stored, err := e.d.Store.AppendChatMessage(ctx, msg)
		if err != nil {
			return err
		}
		e.d.Bus.Emit(ctx, domain.EvChatMessage, conv.Project, "", "", "heartbeat queued for the orchestrator", stored)
		// The turn may have ended before the message was stored; nobody would send it then.
		if now, err := e.d.Store.GetConversation(ctx, conv.ID); err == nil && !now.Busy {
			if err := e.FlushChat(ctx, conv.ID); err != nil && !errors.Is(err, errStopped) {
				return err
			}
		}
		return nil
	}

	queued, err := e.claimQueued(ctx, conv.ID)
	if err != nil {
		e.releaseConversation(ctx, conv.ID)
		return err
	}
	stored, err := e.d.Store.AppendChatMessage(ctx, msg)
	if err != nil {
		_, ids := messageTexts(queued)
		e.restoreQueued(ctx, ids)
		e.releaseConversation(ctx, conv.ID)
		return err
	}
	e.d.Bus.Emit(ctx, domain.EvChatMessage, conv.Project, "", "", "heartbeat: orchestrator woken", stored)
	texts, ids := chatTexts(queued)
	texts, ids = append(texts, prompt), append(ids, stored.ID)
	if err := e.startTurn(ctx, conv, texts, ids); err != nil {
		_, qids := messageTexts(queued)
		e.restoreQueued(ctx, qids)
		return err
	}
	return nil
}

// heartbeatPrompt is the internal message the Operations turn is given. Item summaries quote
// agent text, so they are redacted and sit under the evidence note: data, not instructions.
func heartbeatPrompt(d HeartbeatDigest, cfg *config.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[onett] Heartbeat: %d item(s) need attention (digest built %s). %s\n",
		len(d.Items), d.At.UTC().Format("2006-01-02 15:04 MST"), evidenceNote)
	for _, it := range d.Items {
		mark := ""
		if slices.Contains(d.NewKeys, it.Key) {
			mark = " [NEW]"
		}
		proj := cfg.Projects[it.Project]
		if proj == nil {
			proj = &config.Project{} // a project removed since the digest: no project env to redact
		}
		summary := firstRunes(redactSecrets(oneLineText(it.Summary), evidenceEnv(proj)), hbSummaryLimit)
		fmt.Fprintf(&b, "\n- %s%s: %s (project %s, id %s, first seen %s)",
			it.Kind, mark, summary, it.Project, it.EntityID, it.FirstSeen.UTC().Format("2006-01-02 15:04 MST"))
		if it.Heartbeats > 0 {
			fmt.Fprintf(&b, " [heartbeat %d]", it.Heartbeats)
		}
		if it.Escalated {
			b.WriteString(" [owner already notified]")
		}
		for _, a := range it.Actions {
			fmt.Fprintf(&b, "\n    previously: %s", firstRunes(redactSecrets(oneLineText(a), evidenceEnv(proj)), hbSummaryLimit))
		}
	}
	b.WriteString(`

You are the operator on duty. Look at each item and act only within what the owner already asked for:
- retry a task with a precise note when its cause is fixed or fixable;
- cancel duplicate or superseded work;
- re-queue a known dependency;
- file one small fix task (kind bugfix or chore) for a systemic cause;
- resume or unblock a goal when its blocker is gone;
- message a task agent to answer its question when the answer is already known.

Do not start new product work. Inspect before you act (` + "`onett task show`, `onett run logs`" + `); never guess.

Do not repeat an action already listed for an item ("previously:"); if every useful action has been tried, escalate. An item that stays open for several heartbeats is escalated to the owner by onett itself.

End your reply with one short line per item, exactly in this form:
<entity id>: <action taken | left alone: why | escalate: why>

Use "escalate:" only for a hard limit (the same permission denied again and again, secrets or credentials, the pay-per-token budget) or a decision only the owner can make.`)
	return b.String()
}
