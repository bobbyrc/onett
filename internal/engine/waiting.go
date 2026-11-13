package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// A task can wait on other tasks (Task.WaitingOn): it stays queued but the store will not claim
// it. A follow-up waits for the task that filed it, because its worktree is cut from the default
// branch and has none of that task's code until the pull request merges. An agent that is blocked
// by another task's unmerged work reports depends_on and waits the same way.

// depOutcome says how a task a waiter depends on stands.
type depOutcome int

const (
	depPending depOutcome = iota // still in flight: queued, running, in review, awaiting approval
	depLanded                    // merged, deploying, or done (with or without a pull request)
	depGone                      // blocked, failed or cancelled: it will not land by itself
)

func dependencyOutcome(t domain.Task) depOutcome {
	switch t.Status {
	case domain.TaskMerged, domain.TaskDeploying, domain.TaskDone:
		return depLanded
	case domain.TaskBlocked, domain.TaskFailed, domain.TaskCancelled:
		return depGone
	}
	return depPending
}

// inheritFromParent gives a new child task its parent's goal, and makes it wait for the parent
// when the parent has not landed. Review tasks are exempt: they review the parent's open pull
// request, so waiting for the merge would deadlock.
func (e *Engine) inheritFromParent(ctx context.Context, t *domain.Task) {
	if t.ParentID == "" || t.Kind == domain.KindReview {
		return
	}
	parent, err := e.d.Store.GetTask(ctx, t.ParentID)
	if err != nil {
		return
	}
	if t.GoalID == "" {
		t.GoalID = parent.GoalID
	}
	if len(t.WaitingOn) == 0 && dependencyOutcome(parent) == depPending {
		t.WaitingOn = []string{parent.ID}
	}
}

// checkWaitingOn validates the waiting_on a client supplied when it created a task. An unknown
// task, or one that ended blocked, failed or cancelled and so will not merge, is an error; one that
// has already landed is dropped, because there is nothing left to wait for. It returns the tasks
// still in flight, without repeats.
func (e *Engine) checkWaitingOn(ctx context.Context, ids []string) ([]string, error) {
	var pending []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || slices.Contains(pending, id) {
			continue
		}
		dep, err := e.d.Store.GetTask(ctx, id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("%w: waiting_on: unknown task %q", api.ErrInvalid, id)
			}
			return nil, err
		}
		switch dependencyOutcome(dep) {
		case depGone:
			return nil, fmt.Errorf("%w: waiting_on: task %s is %s and will not merge", api.ErrInvalid, id, dep.Status)
		case depPending:
			pending = append(pending, id)
		}
	}
	return pending, nil
}

// releaseWaiting clears the entries of WaitingOn whose task has landed, so the scheduler may
// claim the task, and cancels a waiter whose dependency ended without landing.
func (e *Engine) releaseWaiting(ctx context.Context) {
	queued, err := e.d.Store.ListTasks(ctx, store.TaskFilter{Statuses: []domain.TaskStatus{domain.TaskQueued}, Limit: 500})
	if err != nil {
		// A shutdown that cancels the query is not an error worth logging.
		if ctx.Err() == nil {
			e.d.Logger.Error("list waiting tasks", "err", err)
		}
		return
	}
	for _, t := range queued {
		if len(t.WaitingOn) > 0 {
			e.checkWaiter(ctx, t)
		}
	}
}

func (e *Engine) checkWaiter(ctx context.Context, t domain.Task) {
	var landed []string
	for _, id := range t.WaitingOn {
		dep, err := e.d.Store.GetTask(ctx, id)
		if err != nil {
			landed = append(landed, id) // a task that no longer exists cannot be waited for
			continue
		}
		switch dependencyOutcome(dep) {
		case depGone:
			reason := fmt.Sprintf("cancelled: it waited for task %s, which is %s and will not merge", dep.ID, dep.Status)
			_, _, err := e.setTaskFrom(ctx, t.ID, []domain.TaskStatus{domain.TaskQueued}, reason, func(x *domain.Task) error {
				x.Status = domain.TaskCancelled
				x.WaitingOn = nil
				x.BlockedReason = reason
				return nil
			})
			if err != nil {
				e.d.Logger.Error("cancel waiting task", "task", t.ID, "err", err)
			}
			return
		case depLanded:
			landed = append(landed, id)
		}
	}
	if len(landed) == 0 {
		return
	}
	_, _, err := e.setTaskFrom(ctx, t.ID, []domain.TaskStatus{domain.TaskQueued}, "dependencies landed: "+strings.Join(landed, ", "),
		func(x *domain.Task) error {
			x.WaitingOn = slices.DeleteFunc(slices.Clone(x.WaitingOn), func(id string) bool { return slices.Contains(landed, id) })
			if len(x.WaitingOn) == 0 {
				x.Body = strings.TrimRight(x.Body, "\n") + fmt.Sprintf(
					"\n\n## Dependencies landed\n\nTask(s) %s you were waiting for have merged into the default branch; "+
						"your worktree includes their code now.\n", strings.Join(t.WaitingOn, ", "))
			}
			return nil
		})
	if err != nil {
		e.d.Logger.Error("release waiting task", "task", t.ID, "err", err)
	}
}

// waitOnDependencies handles a run that ended blocked: when its result names tasks that are still
// in flight, the task goes back to the queue waiting on them instead of blocking. It reports
// whether it did. The attempt does not count, since the agent did nothing wrong.
func (e *Engine) waitOnDependencies(ctx context.Context, t domain.Task, result domain.RunResult) bool {
	var pending []string
	for _, id := range result.DependsOn {
		id = strings.TrimSpace(id)
		if id == "" || id == t.ID || slices.Contains(pending, id) {
			continue
		}
		dep, err := e.d.Store.GetTask(ctx, id)
		if err != nil || dependencyOutcome(dep) != depPending {
			continue
		}
		pending = append(pending, id)
	}
	if len(pending) == 0 {
		return false
	}
	reason := "waiting for " + strings.Join(pending, ", ") + " to merge"
	_, applied, err := e.setTaskFrom(ctx, t.ID, []domain.TaskStatus{domain.TaskRunning}, reason, func(x *domain.Task) error {
		x.Status = domain.TaskQueued
		x.WaitingOn = pending
		x.BlockedReason = ""
		if x.Attempts > 0 {
			x.Attempts--
		}
		return nil
	})
	if err != nil {
		e.d.Logger.Error("wait on dependencies", "task", t.ID, "err", err)
		return false
	}
	return applied
}
