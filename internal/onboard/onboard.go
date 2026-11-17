// Package onboard holds the checks behind adding a project: it lists the owner's GitHub
// repositories, copies a repository into onett's data directory, reads its default branch, checks
// that onett can open pull requests, and guesses setup and checks from its files. The API and the
// CLI both call it. Detection only reads files: it never runs repository code.
package onboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bobbyrc/onett/internal/redact"
	"github.com/bobbyrc/onett/internal/workspace"
)

var (
	ErrInvalid    = errors.New("invalid repository")     // bad repo text or derived name
	ErrExists     = errors.New("project already exists") // name or repository already configured
	ErrInUse      = errors.New("copy is in use")         // Discard of a name a project uses
	ErrNotFound   = errors.New("no copy to discard")     // Discard of a name with no copy made by this Service
	ErrBusy       = errors.New("already being checked")  // Inspect of a name another Inspect is working on
	ErrNotChecked = errors.New("repository not checked") // Claim of a repository with no ready Inspect result
)

// RunGH runs the GitHub CLI and returns its stdout. Errors include its stderr.
type RunGH func(ctx context.Context, args ...string) ([]byte, error)

// Service runs the checks. Copies it makes are remembered until a project claims them or they
// are discarded.
type Service struct {
	ws *workspace.Manager
	gh RunGH

	mu      sync.Mutex
	made    map[string]string // name -> path of copies this Service created and no project has claimed
	checked map[string]Result // name -> last Ready result
	busy    map[string]bool   // names an Inspect is working on
}

// New returns a Service that copies with ws and runs gh with gh. A nil gh runs the real `gh`.
func New(ws *workspace.Manager, gh RunGH) *Service {
	if gh == nil {
		gh = runRealGH
	}
	return &Service{
		ws:      ws,
		gh:      gh,
		made:    map[string]string{},
		checked: map[string]Result{},
		busy:    map[string]bool{},
	}
}

// runRealGH runs the `gh` on PATH. Its stderr goes into the error, because that is where gh says
// why it failed.
func runRealGH(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

type Command struct {
	Name string `json:"name"`
	Run  string `json:"run"`
	From string `json:"from,omitempty"` // the file the guess came from, e.g. "package-lock.json"
}

type Guess struct {
	Setup    []Command `json:"setup"`     // never nil (empty slice)
	Checks   []Command `json:"checks"`    // never nil
	TopFiles []string  `json:"top_files"` // never nil
}

type CopyResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"` // redacted
}

type PullRequests struct {
	OK     bool   `json:"ok"`
	GHUser string `json:"gh_user"`
	Error  string `json:"error"`
}

type Result struct {
	Name          string       `json:"name"`
	Repo          string       `json:"repo"`   // normalized
	GitHub        bool         `json:"github"` // repo is owner/name
	Private       bool         `json:"private"`
	Copy          CopyResult   `json:"copy"`
	DefaultBranch string       `json:"default_branch"`
	BranchFrom    string       `json:"branch_from"` // "github", "repository" or ""
	PullRequests  PullRequests `json:"pull_requests"`
	Guess         Guess        `json:"guess"`
	Suggest       string       `json:"suggest"` // a close match from the owner's repositories when the copy failed, or ""
}

// Ready reports whether the project may be added: copied, branch known, and (for a GitHub repository) PRs possible.
func (r Result) Ready() bool {
	return r.Copy.OK && r.DefaultBranch != "" && (!r.GitHub || r.PullRequests.OK)
}

type Step struct {
	Name   string `json:"name"`  // "copy", "branch", "pull_requests", "guess"
	State  string `json:"state"` // "running", "done", "failed", "skipped"
	Detail string `json:"detail,omitempty"`
}

type Request struct {
	Repo     string            // as the person typed it
	DataDir  string            // the copy goes to DataDir/repos/<name>
	Existing map[string]string // configured projects: name -> repo
}

// Inspect runs the checks for req.Repo and reports each step through progress. Errors it returns
// are about the request itself and come before any work. A failed copy is not an error: it is
// reported in the Result.
func (s *Service) Inspect(ctx context.Context, req Request, progress func(Step)) (Result, error) {
	if progress == nil {
		progress = func(Step) {}
	}
	repo, err := NormalizeRepo(req.Repo)
	if err != nil {
		return Result{}, err
	}
	name := NameFor(repo)
	if _, ok := req.Existing[name]; ok {
		return Result{}, fmt.Errorf("%w: a project named %s already exists", ErrExists, name)
	}
	for other, r := range req.Existing {
		if r == repo {
			return Result{}, fmt.Errorf("%w: project %s already works on %s", ErrExists, other, repo)
		}
	}
	s.mu.Lock()
	if s.busy[name] {
		s.mu.Unlock()
		return Result{}, fmt.Errorf("%w: %s", ErrBusy, name)
	}
	s.busy[name] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.busy, name)
		s.mu.Unlock()
	}()

	// Every path returns this Guess's slices as [] rather than null, so the JSON the UI reads
	// always holds arrays, even when the copy failed and no guess was made.
	res := Result{Name: name, Repo: repo, GitHub: IsGitHub(repo),
		Guess: Guess{Setup: []Command{}, Checks: []Command{}, TopFiles: []string{}}}
	// gh is asked once. When the owner is signed out, no later step calls it.
	var user string
	signedIn := false
	if res.GitHub {
		if _, err := s.gh(ctx, "auth", "status"); err == nil {
			signedIn = true
			if out, err := s.gh(ctx, "api", "user", "--jq", ".login"); err == nil {
				user = strings.TrimSpace(string(out))
			}
		}
	}

	progress(Step{Name: "copy", State: "running"})
	path := filepath.Join(req.DataDir, "repos", name)
	r := workspace.Repo{Name: name, Remote: repo, Path: path}
	created, err := s.ws.PrepareCopy(ctx, r)
	if created {
		s.mu.Lock()
		s.made[name] = path
		s.mu.Unlock()
		// The client that started this check may be gone (dialog closed, Ctrl-C) and has no name
		// to discard the copy with, so a cancelled check removes the copy it made.
		defer func() {
			if ctx.Err() == nil {
				return
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			os.RemoveAll(path)
			delete(s.made, name)
			delete(s.checked, name)
		}()
	}
	if err != nil {
		msg := redact.Secrets(err.Error(), os.Environ())
		res.Copy = CopyResult{OK: false, Error: msg}
		// The signed-in account is named even here, so the UI can say which login to check.
		res.PullRequests = PullRequests{GHUser: user}
		progress(Step{Name: "copy", State: "failed", Detail: msg})
		for _, step := range []string{"branch", "pull_requests", "guess"} {
			progress(Step{Name: step, State: "skipped"})
		}
		if res.GitHub && signedIn {
			res.Suggest = suggestRepo(repo, s.Repos(ctx, req.Existing).Repos)
		}
		return res, nil
	}
	res.Copy = CopyResult{OK: true}
	progress(Step{Name: "copy", State: "done"})

	progress(Step{Name: "branch", State: "running"})
	if res.GitHub && signedIn {
		if out, err := s.gh(ctx, "repo", "view", repo, "--json", "defaultBranchRef,isPrivate"); err == nil {
			var view struct {
				DefaultBranchRef struct {
					Name string `json:"name"`
				} `json:"defaultBranchRef"`
				IsPrivate bool `json:"isPrivate"`
			}
			if json.Unmarshal(out, &view) == nil && view.DefaultBranchRef.Name != "" {
				res.DefaultBranch = view.DefaultBranchRef.Name
				res.Private = view.IsPrivate
				res.BranchFrom = "github"
			}
		}
	}
	var branchErr error
	if res.DefaultBranch == "" {
		if branch, err := s.ws.RemoteDefaultBranch(ctx, r); err != nil {
			branchErr = err
		} else {
			res.DefaultBranch = branch
			res.BranchFrom = "repository"
		}
	}
	if branchErr == nil {
		r.DefaultBranch = res.DefaultBranch
		branchErr = s.ws.EnsureClone(ctx, r)
	}
	if branchErr != nil {
		res.DefaultBranch = ""
		res.BranchFrom = ""
		msg := redact.Secrets(branchErr.Error(), os.Environ())
		progress(Step{Name: "branch", State: "failed", Detail: msg})
	} else {
		progress(Step{Name: "branch", State: "done", Detail: res.DefaultBranch})
	}

	progress(Step{Name: "pull_requests", State: "running"})
	switch {
	case !res.GitHub:
		res.PullRequests = PullRequests{Error: "not a GitHub repository: onett can work on it but cannot open pull requests"}
		progress(Step{Name: "pull_requests", State: "skipped"})
	case signedIn:
		res.PullRequests = PullRequests{OK: true, GHUser: user}
		progress(Step{Name: "pull_requests", State: "done"})
	default:
		msg := "the GitHub CLI is not signed in on this machine; run gh auth login"
		res.PullRequests = PullRequests{Error: msg}
		progress(Step{Name: "pull_requests", State: "failed", Detail: msg})
	}

	progress(Step{Name: "guess", State: "running"})
	res.Guess = GuessCommands(path)
	progress(Step{Name: "guess", State: "done"})

	if res.Ready() {
		s.mu.Lock()
		s.checked[name] = res
		s.mu.Unlock()
	}
	return res, nil
}

// Checked returns the last Ready result for repo, if an Inspect made one.
func (s *Service) Checked(repo string) (Result, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.checked {
		if r.Repo == repo {
			return r, true
		}
	}
	return Result{}, false
}

// Claim gives the checked copy of repo the name name, once a project is saved under it. The copy
// is renamed when the names differ, and the Service forgets it: the project owns it now.
func (s *Service) Claim(dataDir, repo, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res Result
	found := false
	for _, r := range s.checked {
		if r.Repo == repo {
			res, found = r, true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrNotChecked, repo)
	}
	if name != res.Name {
		from := filepath.Join(dataDir, "repos", res.Name)
		to := filepath.Join(dataDir, "repos", name)
		if _, err := os.Lstat(to); err == nil {
			return fmt.Errorf("%w: %s already exists", ErrExists, to)
		}
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("rename copy %s to %s: %w", from, to, err)
		}
	}
	delete(s.checked, res.Name)
	delete(s.made, res.Name)
	return nil
}

// Discard removes the copy this Service made for name. A name a configured project uses is
// refused, because the project depends on that copy.
func (s *Service) Discard(name string, existing map[string]string) error {
	if _, ok := existing[name]; ok {
		return fmt.Errorf("%w: %s is a configured project", ErrInUse, name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path, ok := s.made[name]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove copy %s: %w", path, err)
	}
	delete(s.made, name)
	delete(s.checked, name)
	return nil
}

type GHStatus struct {
	SignedIn bool   `json:"signed_in"`
	User     string `json:"user"`
	Host     string `json:"host"`            // short host name of this machine (os.Hostname up to the first '.')
	Fix      string `json:"fix,omitempty"`   // "gh auth login" when not signed in
	Error    string `json:"error,omitempty"` // a signed-in list that failed
}

type GitHubRepo struct {
	FullName      string    `json:"full_name"`
	Private       bool      `json:"private"`
	Description   string    `json:"description"`
	DefaultBranch string    `json:"default_branch"`
	PushedAt      time.Time `json:"pushed_at"`
	Project       *string   `json:"project"` // the configured project that uses it, or null
}

type ReposResult struct {
	GH    GHStatus     `json:"gh"`
	Repos []GitHubRepo `json:"repos"` // never nil
}

// Repos lists the owner's GitHub repositories, most recently pushed first, and says which of
// them a configured project already uses.
func (s *Service) Repos(ctx context.Context, existing map[string]string) ReposResult {
	host := hostName()
	out := ReposResult{Repos: []GitHubRepo{}}
	if _, err := s.gh(ctx, "auth", "status"); err != nil {
		out.GH = GHStatus{SignedIn: false, Fix: "gh auth login", Host: host}
		return out
	}
	out.GH = GHStatus{SignedIn: true, User: s.ghUser(ctx), Host: host}
	raw, err := s.gh(ctx, "repo", "list", "--limit", "100", "--json",
		"nameWithOwner,isPrivate,description,defaultBranchRef,pushedAt")
	if err != nil {
		out.GH.Error = redact.Secrets(err.Error(), os.Environ())
		return out
	}
	var items []struct {
		NameWithOwner    string `json:"nameWithOwner"`
		IsPrivate        bool   `json:"isPrivate"`
		Description      string `json:"description"`
		DefaultBranchRef struct {
			Name string `json:"name"`
		} `json:"defaultBranchRef"`
		PushedAt time.Time `json:"pushedAt"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		out.GH.Error = redact.Secrets(err.Error(), os.Environ())
		return out
	}
	repos := make([]GitHubRepo, 0, len(items))
	for _, it := range items {
		r := GitHubRepo{
			FullName:      it.NameWithOwner,
			Private:       it.IsPrivate,
			Description:   it.Description,
			DefaultBranch: it.DefaultBranchRef.Name,
			PushedAt:      it.PushedAt,
		}
		for name, repo := range existing {
			if repo == it.NameWithOwner {
				p := name
				r.Project = &p
			}
		}
		repos = append(repos, r)
	}
	sort.SliceStable(repos, func(i, j int) bool { return repos[i].PushedAt.After(repos[j].PushedAt) })
	out.Repos = repos
	return out
}

// ghUser returns the signed-in GitHub login, or "" when gh cannot say.
func (s *Service) ghUser(ctx context.Context) string {
	out, err := s.gh(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// hostName is this machine's short host name.
func hostName() string {
	h, _ := os.Hostname()
	if i := strings.Index(h, "."); i >= 0 {
		h = h[:i]
	}
	return h
}
