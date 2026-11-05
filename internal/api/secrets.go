package api

import (
	"errors"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/secrets"
)

// Secrets are owner-only and write-only: the API lists names and where each comes from, and
// sets or deletes a value, but no route returns a value or its length. secrets.env is the
// store; see internal/secrets.

// secretView is one entry of GET /secrets.
type secretView struct {
	Name         string   `json:"name"`
	Set          bool     `json:"set"`
	Source       string   `json:"source"` // "file", "environment" or ""
	ReferencedBy []string `json:"referenced_by"`
}

// secretSaved is the body of PUT /secrets/{name}.
type secretSaved struct {
	Name     string `json:"name"`
	Set      bool   `json:"set"`
	Shadowed bool   `json:"shadowed"`
}

func (s *server) secretStore(w http.ResponseWriter) *secrets.Store {
	if s.opts.Secrets == nil {
		writeErrorMsg(w, http.StatusNotImplemented, "this daemon has no secrets file")
		return nil
	}
	return s.opts.Secrets
}

func (s *server) listSecrets(w http.ResponseWriter, r *http.Request) {
	store := s.secretStore(w)
	if store == nil {
		return
	}
	names, err := store.Names()
	if err != nil {
		s.fail(w, err)
		return
	}
	// Names the config refers to, read from the file as written (before expansion). An
	// unreadable or invalid file only means no references are shown.
	var refs map[string][]string
	if _, text, err := s.readConfigFile(); err == nil {
		if doc, err := config.DocumentTree(text); err == nil {
			refs = secrets.References(doc)
		}
	}
	all := map[string]bool{}
	for _, n := range names {
		all[n] = true
	}
	for n := range refs {
		all[n] = true
	}
	out := make([]secretView, 0, len(all))
	for n := range all {
		src := store.Source(n)
		out = append(out, secretView{Name: n, Set: src != "", Source: src, ReferencedBy: nonNil(refs[n])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"secrets": out})
}

func (s *server) putSecret(w http.ResponseWriter, r *http.Request) {
	store := s.secretStore(w)
	if store == nil {
		return
	}
	var in struct {
		Value string `json:"value"`
	}
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	name := chi.URLParam(r, "name")
	s.configMu.Lock()
	defer s.configMu.Unlock()
	shadowed, err := store.Set(name, in.Value)
	if err != nil {
		s.failSecret(w, err)
		return
	}
	if err := s.opts.Controller.ReloadConfig(r.Context()); err != nil {
		s.log.Error("api: reload after secret save", "name", name, "err", err)
		writeErrorMsg(w, http.StatusInternalServerError,
			"the secret was saved, but the daemon could not reload the config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, secretSaved{Name: name, Set: true, Shadowed: shadowed})
}

func (s *server) deleteSecret(w http.ResponseWriter, r *http.Request) {
	store := s.secretStore(w)
	if store == nil {
		return
	}
	name := chi.URLParam(r, "name")
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := store.Delete(name); err != nil {
		s.failSecret(w, err)
		return
	}
	if err := s.opts.Controller.ReloadConfig(r.Context()); err != nil {
		s.log.Error("api: reload after secret delete", "name", name, "err", err)
		writeErrorMsg(w, http.StatusInternalServerError,
			"the secret was removed, but the daemon could not reload the config: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// failSecret maps the secrets package's errors to statuses. Their messages never hold a value.
func (s *server) failSecret(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, secrets.ErrInvalid):
		writeErrorMsg(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, secrets.ErrNotFound):
		writeErrorMsg(w, http.StatusNotFound, "that secret is not in the secrets file")
	default:
		s.fail(w, err)
	}
}
