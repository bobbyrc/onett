package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/notify"
	"github.com/bobbyrc/onett/internal/store"
)

// CreateGoal implements api.Controller. The goal gets its own orchestrator conversation, and the
// goals loop runs the first turn on its next tick.
func (e *Engine) CreateGoal(ctx context.Context, g domain.Goal) (domain.Goal, error) {
	if _, err := e.project(g.Project); err != nil {
		return domain.Goal{}, err
	}
	g.Title, g.Body = strings.TrimSpace(g.Title), strings.TrimSpace(g.Body)
	if g.Title == "" {
		return domain.Goal{}, fmt.Errorf("%w: title is required", api.ErrInvalid)
	}
	g.ID, g.Status, g.ConversationID, g.Turns, g.LastTurnAt, g.Summary = "", domain.GoalActive, "", 0, nil, ""
	g, err := e.d.Store.CreateGoal(ctx, g)
	if err != nil {
		return domain.Goal{}, err
	}
	conv, err := e.d.Store.CreateConversation(ctx, domain.Conversation{
		Title: g.Title, Project: g.Project, GoalID: g.ID,
	})
	if err == nil {
		g, err = e.d.Store.UpdateGoal(ctx, g.ID, func(g *domain.Goal) error {
			g.ConversationID = conv.ID
			return nil
		})
	}
	if err != nil {
		// A goal without a conversation could never take a turn; do not leave it active.
		_, _ = e.moveGoal(context.WithoutCancel(ctx), g.ID, domain.GoalBlocked, "could not create its conversation: "+err.Error(), domain.GoalActive)
		return domain.Goal{}, err
	}
	e.emitGoal(ctx, g, "goal created: "+g.Title)
	e.kickLoop(loopGoals, g.Project)
	return g, nil
}

// goalMoves lists the statuses a goal may move to another status from.
var goalMoves = map[domain.GoalStatus][]domain.GoalStatus{
	domain.GoalPaused:  {domain.GoalActive},
	domain.GoalActive:  {domain.GoalPaused, domain.GoalBlocked},
	domain.GoalDone:    {domain.GoalActive, domain.GoalPaused, domain.GoalBlocked},
	domain.GoalBlocked: {domain.GoalActive, domain.GoalPaused},
}

// SetGoalStatus implements api.Controller.
func (e *Engine) SetGoalStatus(ctx context.Context, id string, to domain.GoalStatus, text string) (domain.Goal, error) {
	text = strings.TrimSpace(text)
	if (to == domain.GoalDone || to == domain.GoalBlocked) && text == "" {
		what := "summary"
		if to == domain.GoalBlocked {
			what = "reason"
		}
		return domain.Goal{}, fmt.Errorf("%w: a %s is required", api.ErrInvalid, what)
	}
	var from []domain.GoalStatus
	if to == domain.GoalActive { // resume
		from = []domain.GoalStatus{domain.GoalPaused, domain.GoalBlocked}
	} else {
		from = goalMoves[to]
	}
	return e.moveGoal(ctx, id, to, text, from...)
}

// moveGoal sets a goal's status when it is one of from, in one store update, and otherwise
// returns api.ErrConflict. text becomes Summary for done and blocked. Done and blocked notify.
func (e *Engine) moveGoal(ctx context.Context, id string, to domain.GoalStatus, text string, from ...domain.GoalStatus) (domain.Goal, error) {
	g, err := e.d.Store.UpdateGoal(ctx, id, func(g *domain.Goal) error {
		if !slices.Contains(from, g.Status) {
			return fmt.Errorf("%w: goal is %s", api.ErrConflict, g.Status)
		}
		g.Status = to
		if to == domain.GoalDone || to == domain.GoalBlocked {
			g.Summary = text
		}
		return nil
	})
	if err != nil {
		return g, err
	}
	e.emitGoal(ctx, g, fmt.Sprintf("goal %s: %s", g.Status, g.Title))
	path := "/chat/" + g.ConversationID
	switch to {
	case domain.GoalDone:
		e.notify(ctx, notify.PriorityNormal, "Goal done: "+g.Title, g.Summary, path)
	case domain.GoalBlocked:
		e.notify(ctx, notify.PriorityHigh, "Goal blocked: "+g.Title, g.Summary, path)
	}
	if to == domain.GoalActive {
		e.kickLoop(loopGoals, g.Project)
	}
	return g, nil
}

func (e *Engine) emitGoal(ctx context.Context, g domain.Goal, message string) {
	e.d.Bus.Emit(ctx, domain.EvGoalUpdated, g.Project, "", "", message, g)
}

// checkGoalTask checks that a task created for a goal names an existing goal of the same project.
func (e *Engine) checkGoalTask(ctx context.Context, t domain.Task) error {
	if t.GoalID == "" {
		return nil
	}
	g, err := e.d.Store.GetGoal(ctx, t.GoalID)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: unknown goal %q", api.ErrInvalid, t.GoalID)
	}
	if err != nil {
		return err
	}
	if g.Project != t.Project {
		return fmt.Errorf("%w: goal %s belongs to project %s", api.ErrInvalid, g.ID, g.Project)
	}
	return nil
}

// goalInstructions is the section of the orchestrator's instructions for a goal conversation.
func goalInstructions(goalID string) string {
	return fmt.Sprintf(`

## This conversation runs a goal

You are working unattended toward goal %[1]s. onett wakes you with a check-in each time the
previous step has settled; the owner is not prompting you.

- Plan only the next step that can start now. Keep tasks small and independent. New tasks you
  create with `+"`onett task create`"+` are linked to this goal automatically.
- If a task is blocked or failed and the cause is fixed or fixable, retry it with a note
  (`+"`onett task retry ID --note N`"+`); otherwise plan around it.
- Say plainly in your reply what you queued and why, or why you queued nothing.
- Never approve merges, change config or pause the factory. Awaiting approval is the owner's
  decision; the goal waits for it.
- When the goal is met, run `+"`onett goal done %[1]s --summary S`"+`. When only the owner can
  unblock it, run `+"`onett goal block %[1]s --reason R`"+`. A turn that queues nothing, retries
  nothing and does not finish or block the goal blocks it automatically.
`, goalID)
}
