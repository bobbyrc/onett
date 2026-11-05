package api

import (
	"context"
	"net/http"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// taskListItem is a task in GET /tasks, with where it stands in the queue when it is queued
// and claimable. QueuePosition is the overall claim order; ProjectQueuePosition counts only the
// project's own tasks, which is the honest number while that project is at its run limit.
type taskListItem struct {
	domain.Task
	QueuePosition        int `json:"queue_position,omitempty"`
	ProjectQueuePosition int `json:"project_queue_position,omitempty"`
}

// scanAll is how many unfinished tasks a pickup-order listing reads before it sorts and cuts.
const scanAll = 100000

// listTasksPickup lists tasks in pickup order: running, in progress, queued in the scheduler's
// claim order, waiting, blocked, then finished most recent first. Unfinished tasks are all read
// so the order is exact before the limit cuts; finished ones are read newest-created first.
func (s *server) listTasksPickup(ctx context.Context, f store.TaskFilter) ([]taskListItem, error) {
	var live, finished []domain.TaskStatus
	for _, st := range f.Statuses {
		if isFinished(st) {
			finished = append(finished, st)
		} else {
			live = append(live, st)
		}
	}
	if len(f.Statuses) == 0 {
		live = []domain.TaskStatus{domain.TaskQueued, domain.TaskRunning, domain.TaskReview,
			domain.TaskAwaitingApproval, domain.TaskMerged, domain.TaskDeploying, domain.TaskBlocked}
		finished = []domain.TaskStatus{domain.TaskDone, domain.TaskFailed, domain.TaskCancelled}
	}
	var tasks []domain.Task
	if len(live) > 0 {
		lf := f
		lf.Statuses, lf.Limit = live, scanAll
		got, err := s.opts.Store.ListTasks(ctx, lf)
		if err != nil {
			return nil, err
		}
		tasks = got
	}
	if len(finished) > 0 {
		ff := f
		ff.Statuses = finished
		got, err := s.opts.Store.ListTasks(ctx, ff)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, got...)
	}

	// Queue positions count every claimable queued task, not just the filtered ones.
	queued, err := s.opts.Store.ListTasks(ctx, store.TaskFilter{Statuses: []domain.TaskStatus{domain.TaskQueued}, Limit: scanAll})
	if err != nil {
		return nil, err
	}
	pos := store.PickupOrder(queued, nil)

	started, err := s.runStarts(ctx)
	if err != nil {
		return nil, err
	}
	store.PickupOrder(tasks, started)
	if len(tasks) > f.Limit {
		tasks = tasks[:f.Limit]
	}
	out := make([]taskListItem, len(tasks))
	for i, t := range tasks {
		p := pos[t.ID]
		out[i] = taskListItem{Task: t, QueuePosition: p.Overall, ProjectQueuePosition: p.InProject}
	}
	return out, nil
}

// isFinished reports whether st is a finished status.
func isFinished(st domain.TaskStatus) bool {
	return store.GroupOf(domain.Task{Status: st}) == store.GroupFinished
}

// runStarts maps a task id to when its live run started.
func (s *server) runStarts(ctx context.Context) (map[string]time.Time, error) {
	runs, err := s.opts.Store.ListRuns(ctx, store.RunFilter{
		Statuses: []domain.RunStatus{domain.RunStarting, domain.RunRunning}, Limit: scanAll})
	if err != nil {
		return nil, err
	}
	out := map[string]time.Time{}
	for _, r := range runs {
		if old, ok := out[r.TaskID]; !ok || r.StartedAt.Before(old) {
			out[r.TaskID] = r.StartedAt
		}
	}
	return out, nil
}

func (s *server) listTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := queryLimit(q, defaultLimit)
	if err != nil {
		s.fail(w, err)
		return
	}
	f := store.TaskFilter{
		Project:  q.Get("project"),
		Statuses: csvQuery[domain.TaskStatus](q, "status"),
		Kind:     domain.TaskKind(q.Get("kind")),
		GoalID:   q.Get("goal_id"),
		Limit:    limit,
	}
	switch q.Get("sort") {
	case "", "pickup":
		items, err := s.listTasksPickup(r.Context(), f)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, nonNil(items))
	case "created":
		tasks, err := s.opts.Store.ListTasks(r.Context(), f)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, nonNil(tasks))
	default:
		s.fail(w, invalidf("sort must be pickup or created"))
	}
}
