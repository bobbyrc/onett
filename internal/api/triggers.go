package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/bobbyrc/onett/internal/domain"
)

// TriggerView is a trigger as the API returns it: the trigger plus its newest firing, so a list
// page needs one request.
type TriggerView struct {
	domain.Trigger
	LastFiring *domain.TriggerFiring `json:"last_firing,omitempty"`
}

// triggerView adds the trigger's newest firing, if it has fired.
func (s *server) triggerView(ctx context.Context, t domain.Trigger) (TriggerView, error) {
	v := TriggerView{Trigger: t}
	fs, err := s.opts.Store.ListTriggerFirings(ctx, t.ID, 1)
	if err != nil {
		return TriggerView{}, err
	}
	if len(fs) > 0 {
		v.LastFiring = &fs[0]
	}
	return v, nil
}

func (s *server) listTriggers(w http.ResponseWriter, r *http.Request) {
	ts, err := s.opts.Store.ListTriggers(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]TriggerView, 0, len(ts))
	for _, t := range ts {
		v, err := s.triggerView(r.Context(), t)
		if err != nil {
			s.fail(w, err)
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) createTrigger(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string             `json:"name"`
		Kind     domain.TriggerKind `json:"kind"`
		Project  string             `json:"project"`
		Prompt   string             `json:"prompt"`
		Enabled  bool               `json:"enabled"`
		Cron     string             `json:"cron"`
		TimeZone string             `json:"time_zone"`
		Label    string             `json:"label"`
	}
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	t, err := s.opts.Controller.CreateTrigger(r.Context(), domain.Trigger{
		Name: in.Name, Kind: in.Kind, Project: in.Project, Prompt: in.Prompt, Enabled: in.Enabled,
		Cron: in.Cron, TimeZone: in.TimeZone, Label: in.Label,
	}, closedBy(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, TriggerView{Trigger: t})
}

func (s *server) getTrigger(w http.ResponseWriter, r *http.Request) {
	t, err := s.opts.Store.GetTrigger(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	v, err := s.triggerView(r.Context(), t)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *server) patchTrigger(w http.ResponseWriter, r *http.Request) {
	var p domain.TriggerPatch
	if err := readJSON(w, r, &p); err != nil {
		s.fail(w, err)
		return
	}
	t, err := s.opts.Controller.UpdateTrigger(r.Context(), chi.URLParam(r, "id"), p, closedBy(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	v, err := s.triggerView(r.Context(), t)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *server) deleteTrigger(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Controller.DeleteTrigger(r.Context(), chi.URLParam(r, "id"), closedBy(r)); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) runTrigger(w http.ResponseWriter, r *http.Request) {
	f, err := s.opts.Controller.RunTrigger(r.Context(), chi.URLParam(r, "id"), closedBy(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *server) listTriggerFirings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if _, err := s.opts.Store.GetTrigger(ctx, id); err != nil {
		s.fail(w, err)
		return
	}
	limit, err := queryLimit(r.URL.Query(), defaultLimit)
	if err != nil {
		s.fail(w, err)
		return
	}
	fs, err := s.opts.Store.ListTriggerFirings(ctx, id, limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(fs))
}

// listAllTriggerFirings is the log of recent firings across every trigger. A firing carries its
// trigger's ID, not its name: the caller joins it with GET /triggers.
func (s *server) listAllTriggerFirings(w http.ResponseWriter, r *http.Request) {
	limit, err := queryLimit(r.URL.Query(), 50)
	if err != nil {
		s.fail(w, err)
		return
	}
	fs, err := s.opts.Store.ListAllTriggerFirings(r.Context(), limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(fs))
}
