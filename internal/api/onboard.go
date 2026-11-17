package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/bobbyrc/onett/internal/onboard"
	"github.com/bobbyrc/onett/internal/store"
)

// projectRepos maps each configured project to its repository, for the checks that refuse a
// repository or name already in use.
func (s *server) projectRepos() map[string]string {
	out := map[string]string{}
	for name, p := range s.opts.Controller.Config().Projects {
		if p != nil {
			out[name] = p.Repo
		}
	}
	return out
}

// noOnboard answers 501 and returns true when no onboard.Service is set up.
func (s *server) noOnboard(w http.ResponseWriter) bool {
	if s.opts.Onboard != nil {
		return false
	}
	writeErrorMsg(w, http.StatusNotImplemented, "adding projects is not set up on this server")
	return true
}

// onboardErr maps the errors of internal/onboard onto the API's status codes (see writeError).
func onboardErr(err error) error {
	switch {
	case errors.Is(err, onboard.ErrInvalid):
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	case errors.Is(err, onboard.ErrExists), errors.Is(err, onboard.ErrBusy),
		errors.Is(err, onboard.ErrInUse), errors.Is(err, onboard.ErrNotChecked):
		return fmt.Errorf("%w: %w", ErrConflict, err)
	case errors.Is(err, onboard.ErrNotFound):
		return fmt.Errorf("%w: %w", store.ErrNotFound, err)
	}
	return err
}

// listGithubRepos answers GET /github/repos: the owner's GitHub repositories, for the add-project
// picker. Signed out, it says how to sign in.
func (s *server) listGithubRepos(w http.ResponseWriter, r *http.Request) {
	if s.noOnboard(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.opts.Onboard.Repos(r.Context(), s.projectRepos()))
}

// inspectProject answers POST /projects/inspect. It copies the repository and reads what onett
// needs to add it. Each step goes out as one JSON line as it finishes, so a slow clone shows
// progress; the last line is the result. Refusals found before any work are plain JSON errors.
func (s *server) inspectProject(w http.ResponseWriter, r *http.Request) {
	if s.noOnboard(w) {
		return
	}
	var in struct {
		Repo string `json:"repo"`
	}
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if strings.TrimSpace(in.Repo) == "" {
		s.fail(w, invalidf("repo is required"))
		return
	}
	flusher, _ := w.(http.Flusher)
	started := false
	enc := json.NewEncoder(w)
	line := func(v any) {
		if !started {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			started = true
		}
		_ = enc.Encode(v) // Encode writes one line ending in "\n"
		if flusher != nil {
			flusher.Flush()
		}
	}
	req := onboard.Request{
		Repo:     in.Repo,
		DataDir:  s.opts.Controller.Config().Server.DataDir,
		Existing: s.projectRepos(),
	}
	res, err := s.opts.Onboard.Inspect(r.Context(), req, func(st onboard.Step) {
		line(map[string]onboard.Step{"step": st})
	})
	switch {
	case err != nil && !started:
		s.fail(w, onboardErr(err))
	case err != nil:
		line(map[string]string{"error": err.Error()})
	default:
		line(map[string]onboard.Result{"result": res})
	}
}

// discardInspect answers DELETE /projects/inspect/{name}: it removes a copy that inspect made and
// no project claimed, such as when the person cancels the add.
func (s *server) discardInspect(w http.ResponseWriter, r *http.Request) {
	if s.noOnboard(w) {
		return
	}
	if err := s.opts.Onboard.Discard(chi.URLParam(r, "name"), s.projectRepos()); err != nil {
		s.fail(w, onboardErr(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
