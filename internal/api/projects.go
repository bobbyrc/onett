package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/onboard"
	"github.com/bobbyrc/onett/internal/store"
)

// newProject is the body of POST /projects. Only repo is required; a field left out keeps the
// project's default. The repository must have been checked with POST /projects/inspect first,
// which made the copy this project uses. No clone is made here.
type newProject struct {
	Name         string       `json:"name"`
	Repo         string       `json:"repo"`
	Branch       string       `json:"branch"`
	Policy       string       `json:"policy"`
	Instructions string       `json:"instructions"`
	Setup        []newCommand `json:"setup"`
	Checks       []newCommand `json:"checks"`
}

// newCommand is one setup or checks command of newProject.
type newCommand struct {
	Name string `json:"name"`
	Run  string `json:"run"`
}

// createProject adds a project to the config file through the same save path as PATCH
// /config/document, so it is validated, written atomically and reloaded. Once saved, the checked
// copy becomes the project's (see onboard.Service.Claim).
func (s *server) createProject(w http.ResponseWriter, r *http.Request) {
	if s.noOnboard(w) {
		return
	}
	var in newProject
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	repo, err := onboard.NormalizeRepo(in.Repo)
	if err != nil {
		s.fail(w, onboardErr(err))
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = onboard.NameFor(repo)
	}
	res, ok := s.opts.Onboard.Checked(repo)
	if !ok {
		s.fail(w, fmt.Errorf("%w: check the repository first: POST /projects/inspect", ErrConflict))
		return
	}
	branch := strings.TrimSpace(in.Branch)
	if branch == "" {
		branch = res.DefaultBranch
	}
	dataDir := s.opts.Controller.Config().Server.DataDir
	if dest := filepath.Join(dataDir, "repos", name); name != res.Name {
		if _, err := os.Stat(dest); err == nil {
			s.fail(w, fmt.Errorf("%w: %s already exists", ErrConflict, dest))
			return
		}
	}
	s.saveConfigAs(w, r, nil, func(current []byte) ([]byte, error) {
		path := s.opts.Controller.Config().Path
		out, err := config.AddProject(current, path, config.NewProject{
			Name: name, Repo: repo, Branch: branch,
			Policy: strings.TrimSpace(in.Policy), Instructions: in.Instructions,
			Setup: toChecks(in.Setup), Checks: toChecks(in.Checks),
		})
		if err != nil && errors.Is(err, config.ErrProjectExists) {
			return nil, fmt.Errorf("%w: %s", ErrConflict, err)
		}
		return out, err
	}, func(saved configSaved, cfg *config.Config) any {
		if err := s.opts.Onboard.Claim(dataDir, repo, name); err != nil {
			s.log.Warn("could not hand the checked copy to the new project", "project", name, "err", err)
		}
		p := cfg.Redacted().Projects[name]
		if p == nil {
			p = &config.Project{Name: name}
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

// toChecks maps the commands of a request onto config checks. Each command needs a name and a
// command, which config validation checks.
func toChecks(cs []newCommand) []config.Check {
	out := make([]config.Check, 0, len(cs))
	for _, c := range cs {
		out = append(out, config.Check{Name: c.Name, Run: c.Run})
	}
	return out
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
