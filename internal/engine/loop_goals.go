package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// goalsLoop wakes the orchestrator on each active goal of p whose previous step has settled.
// It never waits for a turn: the turn runs in the background and checks its own outcome.
func (e *Engine) goalsLoop(ctx context.Context, p *config.Project) error {
	goals, err := e.d.Store.ListGoals(ctx, store.GoalFilter{
		Project: p.Name, Statuses: []domain.GoalStatus{domain.GoalActive},
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, g := range goals {
		if ctx.Err() != nil {
			break
		}
		if err := e.goalStep(ctx, p, g); err != nil {
			errs = append(errs, fmt.Errorf("goal %s: %w", g.ID, err))
		}
	}
	return errors.Join(errs...)
}

// errConvBusy aborts the update that takes a conversation when it is already busy.
var errConvBusy = errors.New("conversation is busy")

// goalStep starts a turn for g when it is due: nothing it queued is still in flight, the
// conversation is free, and something changed since the last turn.
func (e *Engine) goalStep(ctx context.Context, p *config.Project, g domain.Goal) error {
	if g.ConversationID == "" {
		return nil
	}
	conv, err := e.d.Store.GetConversation(ctx, g.ConversationID)
	if err != nil {
		return err
	}
	if conv.Busy {
		return nil
	}
	tasks, err := e.d.Store.ListTasks(ctx, store.TaskFilter{GoalID: g.ID, Limit: 500})
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if !t.Status.Terminal() {
			return nil // queued, running, in review, awaiting approval, merged or deploying
		}
	}
	// A stalled turn changed nothing, so the second chance is due without waiting for a change.
	if g.Turns > 0 && g.LastTurnAt != nil && g.StalledTurns == 0 {
		due, err := e.goalChanged(ctx, g, tasks)
		if err != nil || !due {
			return err
		}
	}
	cfg := e.Config()
	if g.Turns >= cfg.Limits.MaxGoalTurns {
		_, err := e.moveGoal(ctx, g.ID, domain.GoalBlocked, goalTurnsReason(cfg.Limits.MaxGoalTurns), domain.GoalActive)
		return ignoreConflict(err)
	}
	if b := cfg.Limits.DailyBudgetUSD; b > 0 && !e.withinDailyBudget(ctx, b) {
		return nil
	}

	// Take the conversation, then record the turn, so neither a tick nor an owner message can
	// start a second turn meanwhile.
	conv, err = e.d.Store.UpdateConversation(ctx, conv.ID, func(c *domain.Conversation) error {
		if c.Busy {
			return errConvBusy
		}
		c.Busy = true
		return nil
	})
	if errors.Is(err, errConvBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	now := e.d.Now().UTC()
	g, err = e.d.Store.UpdateGoal(ctx, g.ID, func(g *domain.Goal) error {
		if g.Status != domain.GoalActive {
			return errSkip
		}
		g.Turns++
		g.LastTurnAt = &now
		return nil
	})
	if err != nil {
		e.releaseConversation(ctx, conv.ID)
		return ignoreSkip(err)
	}
	e.emitGoal(ctx, g, fmt.Sprintf("goal turn %d: %s", g.Turns, g.Title))

	prompt := goalCheckIn(g, tasks, evidenceEnv(p))
	msg, err := e.d.Store.AppendChatMessage(ctx, domain.ChatMessage{
		ConversationID: conv.ID, Role: "system", Kind: domain.ChatKindInternal, Text: prompt,
	})
	if err != nil {
		e.releaseConversation(ctx, conv.ID)
		return err
	}
	e.d.Bus.Emit(ctx, domain.EvChatMessage, conv.Project, "", "", "goal check-in", msg)

	before := make(map[string]time.Time, len(tasks))
	for _, t := range tasks {
		before[t.ID] = t.UpdatedAt
	}
	timeout := cfg.Limits.RunTimeout.Duration
	base := e.replyBase()
	started := e.goTracked(func() {
		runCtx, cancel := context.WithTimeout(base, timeout)
		defer cancel()
		if e.chatReply(runCtx, conv, prompt, nil, chatOpts{goal: true}) {
			e.goalGuard(context.WithoutCancel(runCtx), g.ID, before)
		}
	})
	if !started {
		e.releaseConversation(context.WithoutCancel(ctx), conv.ID)
		return errStopped
	}
	return nil
}

// finishGoalTurn is the after-turn check for a goal turn that was reattached after a restart. The
// in-memory snapshot goalStep took is gone, so progress is judged from the goal's last_turn_at: a
// task changed or created since then is progress.
func (e *Engine) finishGoalTurn(ctx context.Context, goalID string) {
	g, err := e.d.Store.GetGoal(ctx, goalID)
	if err != nil || g.LastTurnAt == nil {
		return
	}
	tasks, err := e.d.Store.ListTasks(ctx, store.TaskFilter{GoalID: goalID, Limit: 500})
	if err != nil {
		e.d.Logger.Error("goal guard: list tasks", "goal", goalID, "err", err)
		return
	}
	before := make(map[string]time.Time, len(tasks))
	for _, t := range tasks {
		if !t.UpdatedAt.After(*g.LastTurnAt) {
			before[t.ID] = t.UpdatedAt
		}
	}
	e.goalGuard(ctx, goalID, before)
}

// goalChanged reports whether something happened since the goal's last turn: one of its tasks
// changed, or the owner wrote in its conversation.
func (e *Engine) goalChanged(ctx context.Context, g domain.Goal, tasks []domain.Task) (bool, error) {
	for _, t := range tasks {
		if t.UpdatedAt.After(*g.LastTurnAt) {
			return true, nil
		}
	}
	msgs, err := e.d.Store.ListChatMessages(ctx, g.ConversationID)
	if err != nil {
		return false, err
	}
	for _, m := range msgs {
		if m.Role == "user" && m.CreatedAt.After(*g.LastTurnAt) {
			return true, nil
		}
	}
	return false, nil
}

// goalGuard runs after a goal turn. A goal still active whose turn queued no task, retried none
// and did not finish or block it made no progress: the first such turn earns a second chance (the
// next check-in says so and the loop wakes at once), the second in a row blocks the goal for the
// owner. A goal that has used up its turns is blocked too. before holds each goal task's
// UpdatedAt from before the turn.
func (e *Engine) goalGuard(ctx context.Context, id string, before map[string]time.Time) {
	g, err := e.d.Store.GetGoal(ctx, id)
	if err != nil || g.Status != domain.GoalActive {
		return
	}
	tasks, err := e.d.Store.ListTasks(ctx, store.TaskFilter{GoalID: id, Limit: 500})
	if err != nil {
		e.d.Logger.Error("goal guard: list tasks", "goal", id, "err", err)
		return
	}
	progress := false
	for _, t := range tasks {
		// A new task has no entry; a retried one was updated.
		if prev, ok := before[t.ID]; !ok || !t.UpdatedAt.Equal(prev) {
			progress = true
			break
		}
	}
	reason := ""
	switch {
	case !progress && g.StalledTurns > 0:
		reason = "the orchestrator made no progress on its last turn"
	case g.Turns >= e.Config().Limits.MaxGoalTurns:
		reason = goalTurnsReason(e.Config().Limits.MaxGoalTurns)
	default:
		stalled := 0
		if !progress {
			stalled = 1
		}
		if stalled == g.StalledTurns {
			return
		}
		_, err := e.d.Store.UpdateGoal(ctx, id, func(g *domain.Goal) error {
			if g.Status != domain.GoalActive {
				return errSkip
			}
			g.StalledTurns = stalled
			return nil
		})
		if err = ignoreSkip(err); err != nil {
			e.d.Logger.Error("goal guard: record stall", "goal", id, "err", err)
			return
		}
		if stalled > 0 {
			e.kickLoop(loopGoals, g.Project)
		}
		return
	}
	if _, err := e.moveGoal(ctx, id, domain.GoalBlocked, reason, domain.GoalActive); ignoreConflict(err) != nil {
		e.d.Logger.Error("goal guard: block", "goal", id, "err", err)
	}
}

func goalTurnsReason(max int) string {
	return fmt.Sprintf("the goal used its %d orchestrator turns (limits.max_goal_turns)", max)
}

// ignoreConflict drops api.ErrConflict: the goal moved on (paused, finished) while onett looked.
func ignoreConflict(err error) error {
	if errors.Is(err, api.ErrConflict) {
		return nil
	}
	return err
}

// ignoreSkip drops errSkip, which a guarded update returns to leave the row alone.
func ignoreSkip(err error) error {
	if errors.Is(err, errSkip) {
		return nil
	}
	return err
}

// goalCheckIn is the system message that opens a turn: the goal, how each of its tasks stands,
// and what to decide.
func goalCheckIn(g domain.Goal, tasks []domain.Task, env []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Goal check-in (turn %d). Goal %s: %s\n\n%s\n\n", g.Turns, g.ID, g.Title, g.Body)
	if g.StalledTurns > 0 {
		b.WriteString("Your last turn made no progress: it queued no task, retried none and did not finish or block the " +
			"goal. Change approach now: queue or retry a task, split a blocked one, or call `onett goal done` / " +
			"`onett goal block` with a reason. Another turn without progress blocks the goal.\n\n")
	}
	if len(tasks) == 0 {
		b.WriteString("No task has been queued for this goal yet.\n")
	} else {
		b.WriteString("Tasks for this goal, oldest first. " + evidenceNote + "\n\n")
		for i := len(tasks) - 1; i >= 0; i-- {
			t := tasks[i]
			fmt.Fprintf(&b, "- %s [%s, %s] %s", t.ID, t.Kind, t.Status, t.Title)
			if t.PRNumber > 0 {
				fmt.Fprintf(&b, " (PR #%d)", t.PRNumber)
			}
			b.WriteString("\n")
			if t.BlockedReason != "" {
				fmt.Fprintf(&b, "  blocked: %s\n", oneLine(t.BlockedReason, env))
			}
			if t.Summary != "" {
				fmt.Fprintf(&b, "  summary: %s\n", oneLine(t.Summary, env))
			}
		}
	}
	fmt.Fprintf(&b, "\nDecide the next step: queue the next task(s) with `onett task create` (they are linked to this goal "+
		"automatically), retry a blocked task with a note if the cause is fixed or fixable, or finish with "+
		"`onett goal done %[1]s --summary ...`, or stop with `onett goal block %[1]s --reason ...` when only the "+
		"owner can unblock it.", g.ID)
	return b.String()
}

// oneLine redacts secrets from agent-written text, collapses whitespace so it cannot break the
// list, and caps its length.
func oneLine(s string, env []string) string {
	s = strings.Join(strings.Fields(redactSecrets(s, env)), " ")
	return firstRunes(s, 600)
}
