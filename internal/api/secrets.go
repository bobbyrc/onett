package api

import (
	"errors"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"

	"github.com/bobbyrc/onett/internal/secrets"
)

// Secrets are owner-only (administer), like the config: the API token cannot reach them. The
// routes never return a value, only names. Values go into secrets.env, which onett loads into its
// environment at startup; see internal/secrets.

// secretList is the body of GET /secrets.
type secretList struct {
	Path  string   `json:"path"`
	Names []string `json:"names"`
}

func (s *server) secretsPath(w http.ResponseWriter) (string, bool) {
	if s.opts.SecretsPath == "" {
		writeErrorMsg(w, http.StatusNotFound, "secrets are not available")
		return "", false
	}
	return s.opts.SecretsPath, true
}

func (s *server) listSecrets(w http.ResponseWriter, r *http.Request) {
	path, ok := s.secretsPath(w)
	if !ok {
		return
	}
	names, err := secrets.Names(path)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, secretList{Path: path, Names: names})
}

// putSecret creates or replaces one secret. The value also goes into the running process's
// environment, so the next config reload and the next agent run see it without a restart.
func (s *server) putSecret(w http.ResponseWriter, r *http.Request) {
	path, ok := s.secretsPath(w)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	var in struct {
		Value string `json:"value"`
	}
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if err := secrets.Set(path, name, in.Value); err != nil {
		s.secretError(w, err)
		return
	}
	if err := os.Setenv(name, in.Value); err != nil {
		s.fail(w, err)
		return
	}
	s.log.Info("api: secret saved", "name", name)
	w.WriteHeader(http.StatusNoContent)
}

// deleteSecret removes a secret from the file. The running process keeps its copy until the next
// restart: onett cannot tell a value it loaded from the file from one the operator exported.
func (s *server) deleteSecret(w http.ResponseWriter, r *http.Request) {
	path, ok := s.secretsPath(w)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	found, err := secrets.Delete(path, name)
	if err != nil {
		s.secretError(w, err)
		return
	}
	if !found {
		writeErrorMsg(w, http.StatusNotFound, "no such secret")
		return
	}
	s.log.Info("api: secret removed", "name", name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) secretError(w http.ResponseWriter, err error) {
	if errors.Is(err, secrets.ErrInvalid) {
		writeErrorMsg(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.fail(w, err)
}
