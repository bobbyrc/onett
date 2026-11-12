package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// GoalDetail is the body of GET /goals/{id}: the goal and the tasks queued for it, newest first.
type GoalDetail struct {
	Goal  domain.Goal   `json:"goal"`
	Tasks []domain.Task `json:"tasks"`
}

func (s *server) listGoals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := queryLimit(q, defaultLimit)
	if err != nil {
		s.fail(w, err)
		return
	}
	goals, err := s.opts.Store.ListGoals(r.Context(), store.GoalFilter{
		Project:  q.Get("project"),
		Statuses: csvQuery[domain.GoalStatus](q, "status"),
		Limit:    limit,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(goals))
}

func (s *server) createGoal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Project string `json:"project"`
		Title   string `json:"title"`
		Body    string `json:"body"`
	}
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	g, err := s.opts.Controller.CreateGoal(r.Context(), domain.Goal{Project: in.Project, Title: in.Title, Body: in.Body})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (s *server) getGoal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, err := s.opts.Store.GetGoal(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	tasks, err := s.opts.Store.ListTasks(ctx, store.TaskFilter{GoalID: g.ID, Limit: defaultLimit})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, GoalDetail{Goal: g, Tasks: nonNil(tasks)})
}

// goalStatus handles pause, resume, done and block. Done takes {"summary"} and block takes
// {"reason"}; pause and resume take no body.
func (s *server) goalStatus(to domain.GoalStatus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Summary string `json:"summary"`
			Reason  string `json:"reason"`
		}
		if to == domain.GoalDone || to == domain.GoalBlocked {
			if err := readJSON(w, r, &in); err != nil {
				s.fail(w, err)
				return
			}
		}
		text := in.Summary
		if to == domain.GoalBlocked {
			text = in.Reason
		}
		g, err := s.opts.Controller.SetGoalStatus(r.Context(), chi.URLParam(r, "id"), to, text)
		if err != nil {
			s.failPerson(w, err)
			return
		}
		writeJSON(w, http.StatusOK, g)
	}
}
