package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
)

// errRunNotLive means the run is not registered (it never started or has already ended), so
// there is no agent to hand a message to.
var errRunNotLive = errors.New("run is not live")

// setSession records the live harness session so owner messages can reach it. The run
// supervisor calls it right after h.Start succeeds.
func (ar *activeRun) setSession(s harness.Session) {
	ar.sessMu.Lock()
	ar.sess = s
	ar.sessMu.Unlock()
}

func (ar *activeRun) session() harness.Session {
	ar.sessMu.Lock()
	defer ar.sessMu.Unlock()
	return ar.sess
}

// messageHeading is the heading an agent sees above a message. It names who wrote it, so an
// agent never reads the orchestrator's words as the owner's.
func messageHeading(author string) string {
	if author == domain.AuthorOrchestrator {
		return "## Message from the orchestrator"
	}
	return "## Message from the owner"
}

// messagesPrompt is the text an agent sees for messages: the author's heading, then the message.
// Messages in a row by one author share a heading.
func messagesPrompt(msgs []domain.ChatMessage) string {
	var b strings.Builder
	last := ""
	for i, m := range msgs {
		h := messageHeading(m.Author)
		switch {
		case i == 0:
			b.WriteString(h + "\n\n")
		case h != last:
			b.WriteString("\n\n" + h + "\n\n")
		default:
			b.WriteString("\n\n")
		}
		b.WriteString(m.Text)
		last = h
	}
	return b.String()
}

// deliverToRun sends an owner message to the live agent of run runID; see deliverToRunAs.
func (e *Engine) deliverToRun(ctx context.Context, runID, text string) error {
	return e.deliverToRunAs(ctx, runID, text, "")
}

// deliverToRunAs sends a message from author ("" is the owner) to the live agent of run runID and
// logs the raw text as an owner line, but only once the agent has accepted it. It returns
// errRunNotLive when the run is not registered or has no session yet, and harness.ErrInputClosed
// when the session does not take input or has stopped taking it; the caller then queues the
// message.
func (e *Engine) deliverToRunAs(ctx context.Context, runID, text, author string) error {
	e.runsMu.Lock()
	ar, ok := e.active[runID]
	e.runsMu.Unlock()
	if !ok {
		return errRunNotLive
	}
	sess := ar.session()
	if sess == nil {
		return errRunNotLive
	}
	st, ok := sess.(harness.Steerable)
	if !ok {
		return harness.ErrInputClosed
	}
	// Send can block until the agent reads, so the registry lock is not held here.
	if err := st.Send(messagesPrompt([]domain.ChatMessage{{Text: text, Author: author}})); err != nil {
		if errors.Is(err, harness.ErrInputClosed) {
			return harness.ErrInputClosed
		}
		return fmt.Errorf("send to agent: %w", err)
	}
	e.appendRunLog(ctx, runID, domain.LogOwner, text)
	return nil
}
