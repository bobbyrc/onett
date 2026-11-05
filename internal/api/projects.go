package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// newProject is the body of POST /projects. It mirrors `onett project add`: only name and repo
// are required, and a field left out keeps the project's default. No clone is made: path, when
// empty, defaults to <data_dir>/repos/<name>, exactly as it does for the CLI.
type newProject struct {
	Name         string `json:"name"`
	Repo         string `json:"repo"`
	Path         string `json:"path"`
	Branch       string `json:"branch"`
	Policy       string `json:"policy"`
	Instructions string `json:"instructions"`
}

// createProject adds a project to the config file through the same save path as PATCH
// /config/document, so it is validated, written atomically and reloaded.
func (s *server) createProject(w http.ResponseWriter, r *http.Request) {
	var in newProject
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	in.Name, in.Repo = strings.TrimSpace(in.Name), strings.TrimSpace(in.Repo)
	if in.Name == "" || in.Repo == "" {
		s.fail(w, invalidf("name and repo are required"))
		return
	}
	s.saveConfigAs(w, r, nil, func(current []byte) ([]byte, error) {
		path := s.opts.Controller.Config().Path
		out, err := config.AddProject(current, path, config.NewProject{
			Name: in.Name, Repo: in.Repo, Path: strings.TrimSpace(in.Path),
			Branch: strings.TrimSpace(in.Branch), Policy: strings.TrimSpace(in.Policy),
			Instructions: in.Instructions,
		})
		if err != nil && errors.Is(err, config.ErrProjectExists) {
			return nil, fmt.Errorf("%w: %s", ErrConflict, err)
		}
		return out, err
	}, func(saved configSaved, cfg *config.Config) any {
		p := cfg.Redacted().Projects[in.Name]
		if p == nil {
			p = &config.Project{Name: in.Name}
		}
		counts := make(map[string]int, len(projectCountStatuses))
		for _, st := range projectCountStatuses {
			counts[string(st)] = 0
		}
		observe := p.Observe
		if observe == nil {
			observe = []config.Observer{}
		}
		return struct {
			ProjectView
			configSaved
		}{ProjectView{Project: *p, Observe: observe, Deploy: p.Deploy, Counts: counts}, saved}
	})
}

// deleteProject removes a project's block from the config file and reloads. It refuses with 409
// while the project has queued or running tasks, unless ?force=true. Nothing on disk is deleted:
// not the clone, not the worktrees, not the project's tasks and runs in the database.
func (s *server) deleteProject(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cfg := s.opts.Controller.Config()
	if cfg.Projects[name] == nil {
		s.fail(w, fmt.Errorf("project %q: %w", name, store.ErrNotFound))
		return
	}
	force := r.URL.Query().Get("force") == "true"
	s.saveConfigAs(w, r, nil, func(current []byte) ([]byte, error) {
		if !force {
			busy, err := s.opts.Store.ListTasks(r.Context(), store.TaskFilter{
				Project:  name,
				Statuses: []domain.TaskStatus{domain.TaskQueued, domain.TaskRunning},
				Limit:    1,
			})
			if err != nil {
				return nil, err
			}
			if len(busy) > 0 {
				return nil, fmt.Errorf("%w: project %q has queued or running tasks; cancel them or pass ?force=true", ErrConflict, name)
			}
		}
		changes := []config.Change{{Op: "delete", Path: []string{"projects", name}}}
		if cfg.SelfProject == name {
			// self_project must name a configured project, so it goes with it.
			changes = append(changes, config.Change{Op: "delete", Path: []string{"self_project"}})
		}
		return config.ApplyChanges(current, changes)
	}, func(saved configSaved, _ *config.Config) any { return saved })
}
