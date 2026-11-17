package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/onboard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reloadRecord counts the daemon reloads the add flow asked for. reloadErr, when set, is what
// the next reload returns.
var reloadRecord struct {
	calls   int
	reloadE error
}

// init replaces everything that reaches outside the process for the whole package: the
// onboarding service (clones, gh, GitHub) and the daemon reload. Stdin is the null device, so
// no run in this package is ever a terminal unless a test makes one.
func init() {
	newOnboarder = func(*config.Config) onboarder { return &fakeOnboard{} }
	reloadDaemon = func(*config.Config) error {
		reloadRecord.calls++
		return reloadRecord.reloadE
	}
	if null, err := os.Open(os.DevNull); err == nil {
		os.Stdin = null
	}
}

// fakeOnboard stands in for onboard.Service. Inspect builds a ready result for the repository
// (a GitHub one signed in, as the real service does for a healthy repository) and then lets
// result edit it; progress steps follow the order the real service reports them in.
type fakeOnboard struct {
	result     func(*onboard.Result)
	inspectErr error
	repos      onboard.ReposResult

	inspected []string
	discarded []string
	claimed   [][3]string // dataDir, repo, name
}

func (f *fakeOnboard) Inspect(_ context.Context, req onboard.Request, progress func(onboard.Step)) (onboard.Result, error) {
	f.inspected = append(f.inspected, req.Repo)
	if f.inspectErr != nil {
		return onboard.Result{}, f.inspectErr
	}
	repo, err := onboard.NormalizeRepo(req.Repo)
	if err != nil {
		return onboard.Result{}, err
	}
	res := onboard.Result{
		Name:          onboard.NameFor(repo),
		Repo:          repo,
		GitHub:        onboard.IsGitHub(repo),
		Copy:          onboard.CopyResult{OK: true},
		DefaultBranch: "main",
		BranchFrom:    "github",
		PullRequests:  onboard.PullRequests{OK: true, GHUser: "tester"},
		Guess: onboard.Guess{
			Setup:    []onboard.Command{},
			Checks:   []onboard.Command{},
			TopFiles: []string{},
		},
	}
	if !res.GitHub {
		res.BranchFrom = "repository"
		res.PullRequests = onboard.PullRequests{OK: false, Error: "not a GitHub repository"}
	}
	if f.result != nil {
		f.result(&res)
	}
	if !res.Copy.OK {
		progress(onboard.Step{Name: "copy", State: "failed", Detail: res.Copy.Error})
		progress(onboard.Step{Name: "branch", State: "skipped"})
		progress(onboard.Step{Name: "pull_requests", State: "skipped"})
		return res, nil
	}
	progress(onboard.Step{Name: "copy", State: "done"})
	progress(onboard.Step{Name: "branch", State: "done", Detail: res.DefaultBranch})
	switch {
	case !res.GitHub:
		progress(onboard.Step{Name: "pull_requests", State: "skipped"})
	case res.PullRequests.OK:
		progress(onboard.Step{Name: "pull_requests", State: "done"})
	default:
		progress(onboard.Step{Name: "pull_requests", State: "failed", Detail: res.PullRequests.Error})
	}
	progress(onboard.Step{Name: "guess", State: "done"})
	return res, nil
}

func (f *fakeOnboard) Discard(name string, _ map[string]string) error {
	f.discarded = append(f.discarded, name)
	return nil
}

func (f *fakeOnboard) Claim(dataDir, repo, name string) error {
	f.claimed = append(f.claimed, [3]string{dataDir, repo, name})
	return nil
}

func (f *fakeOnboard) Repos(context.Context, map[string]string) onboard.ReposResult {
	return f.repos
}

// useFake makes the next add flow use f, and restores the package default when the test ends.
func useFake(t *testing.T, f *fakeOnboard) *fakeOnboard {
	t.Helper()
	old := newOnboarder
	newOnboarder = func(*config.Config) onboarder { return f }
	t.Cleanup(func() { newOnboarder = old })
	return f
}

// useReloadErr makes the daemon reload fail with err, and resets the count for this test.
func useReloadErr(t *testing.T, err error) {
	t.Helper()
	oldCalls, oldErr := reloadRecord.calls, reloadRecord.reloadE
	reloadRecord.calls, reloadRecord.reloadE = 0, err
	t.Cleanup(func() { reloadRecord.calls, reloadRecord.reloadE = oldCalls, oldErr })
}

// guessedCommands gives a repository the guess a Node project with a lint and a test script gets.
func guessedCommands(res *onboard.Result) {
	res.Guess.Setup = []onboard.Command{{Name: "npm ci", Run: "npm ci", From: "package-lock.json"}}
	res.Guess.Checks = []onboard.Command{
		{Name: "npm run lint", Run: "npm run lint", From: "package.json"},
		{Name: "npm test", Run: "npm test", From: "package.json"},
	}
	res.Guess.TopFiles = []string{"package.json", "src"}
}

func readAddFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func parseAddFile(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg, err := config.Parse([]byte(readAddFile(t, path)), path)
	require.NoError(t, err)
	return cfg
}

func TestAddWithoutTerminalNeedsAChoice(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	before := readAddFile(t, path)
	f := useFake(t, &fakeOnboard{})

	code, out, errOut := runCLI("project", "add", "acme/demo", "--config", path)
	assert.Equal(t, 1, code, errOut)
	assert.Contains(t, out, "onett found nothing it knows how to install or test.")
	assert.Contains(t, out, "Pass --setup CMD / --check CMD, or --no-checks for none.")
	assert.Contains(t, out, "\nNothing was added.\n")
	assert.Equal(t, []string{"demo"}, f.discarded, "the copy made for this run is removed")
	assert.Equal(t, before, readAddFile(t, path), "nothing is written")
}

func TestAddWithoutTerminalListsTheGuessAndRefuses(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	before := readAddFile(t, path)
	f := useFake(t, &fakeOnboard{result: guessedCommands})

	code, out, errOut := runCLI("project", "add", "acme/pimlo", "--config", path)
	assert.Equal(t, 1, code, errOut)
	assert.Contains(t, out, "onett guessed setup and checks for pimlo:\n  setup npm ci · checks npm run lint, npm test\n")
	assert.Contains(t, out, "Pass --use-guess to accept them, --setup CMD / --check CMD to give your own,")
	assert.Contains(t, out, "\nNothing was added.\n")
	assert.Equal(t, []string{"pimlo"}, f.discarded)
	assert.Equal(t, before, readAddFile(t, path))
}

func TestAddUseGuessWritesSetupAndChecks(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	useFake(t, &fakeOnboard{result: guessedCommands})
	useReloadErr(t, nil)

	code, out, errOut := runCLI("project", "add", "acme/pimlo", "--use-guess", "--yes", "--config", path)
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "Added pimlo to "+path+" and reloaded onett.")
	assert.Contains(t, out, `onett task create --project pimlo --kind question --title "Explain how pimlo is built and tested"`)
	assert.Equal(t, 1, reloadRecord.calls)

	cfg := parseAddFile(t, path)
	p := cfg.Projects["pimlo"]
	assert.Equal(t, "acme/pimlo", p.Repo)
	assert.Equal(t, "main", p.DefaultBranch)
	assert.Equal(t, "supervised", p.Policy)
	require.Len(t, p.Setup, 1)
	assert.Equal(t, "npm ci", p.Setup[0].Run)
	require.Len(t, p.Checks, 2)
	assert.Equal(t, "npm run lint", p.Checks[0].Run)
	assert.Equal(t, "npm test", p.Checks[1].Run)
}

func TestAddNoChecksWritesNeither(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	useFake(t, &fakeOnboard{result: guessedCommands})

	code, _, errOut := runCLI("project", "add", "acme/pimlo", "--no-checks", "--yes", "--config", path)
	require.Equal(t, 0, code, errOut)
	p := parseAddFile(t, path).Projects["pimlo"]
	assert.Empty(t, p.Setup)
	assert.Empty(t, p.Checks)
	assert.NotContains(t, readAddFile(t, path), "setup:")
}

func TestAddSetupAndCheckFlagsWriteThoseExactly(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	useFake(t, &fakeOnboard{result: guessedCommands})

	code, _, errOut := runCLI("project", "add", "acme/home-lab",
		"--setup", "./scripts/bootstrap.sh", "--check", "make lint", "--check", "make test", "--check", "make lint",
		"--yes", "--config", path)
	require.Equal(t, 0, code, errOut)
	p := parseAddFile(t, path).Projects["home-lab"]
	require.Len(t, p.Setup, 1)
	assert.Equal(t, config.Check{Name: "./scripts/bootstrap.sh", Run: "./scripts/bootstrap.sh"}, p.Setup[0])
	require.Len(t, p.Checks, 2, "a repeated check is dropped")
	assert.Equal(t, "make lint", p.Checks[0].Run)
	assert.Equal(t, "make test", p.Checks[1].Run)
}

func TestAddConflictingChoicesAreUsageErrors(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	before := readAddFile(t, path)
	f := useFake(t, &fakeOnboard{})

	for _, extra := range [][]string{
		{"--no-checks", "--use-guess"},
		{"--use-guess", "--setup", "make"},
		{"--no-checks", "--check", "make test"},
		{"--no-checks", "--setup", "make"},
	} {
		args := append([]string{"project", "add", "acme/demo", "--yes", "--config", path}, extra...)
		code, _, errOut := runCLI(args...)
		assert.Equal(t, 2, code, "args %v: %s", extra, errOut)
	}
	assert.Empty(t, f.inspected, "a conflict is found before anything is copied")
	assert.Equal(t, before, readAddFile(t, path))
}

func TestAddPathFlagIsAUsageError(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	before := readAddFile(t, path)
	f := useFake(t, &fakeOnboard{})

	code, _, errOut := runCLI("project", "add", "acme/demo", "--path", "/x", "--no-checks", "--config", path)
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "-path")
	assert.Empty(t, f.inspected)
	assert.Equal(t, before, readAddFile(t, path))
}

func TestAddFailedCopyStopsAndSuggestsTheRightRepository(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	before := readAddFile(t, path)
	f := useFake(t, &fakeOnboard{result: func(r *onboard.Result) {
		r.Copy = onboard.CopyResult{OK: false, Error: "remote: Repository not found.\nEither the name is wrong."}
		r.Suggest = "acme/pimlo"
	}})

	code, out, errOut := runCLI("project", "add", "acme/pimlo-app", "--no-checks", "--yes", "--config", path)
	assert.Equal(t, 1, code, errOut)
	assert.Contains(t, out, "Checking acme/pimlo-app\n")
	assert.Contains(t, out, "  STOP  could not copy the repository:\n          remote: Repository not found.\n          Either the name is wrong.\n")
	assert.Contains(t, out, "        Did you mean acme/pimlo?\n")
	assert.Contains(t, out, "\nNothing was added.\n")
	assert.Equal(t, []string{"pimlo-app"}, f.discarded)
	assert.Equal(t, before, readAddFile(t, path))
}

func TestAddGitHubWithGHSignedOutStops(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	before := readAddFile(t, path)
	useFake(t, &fakeOnboard{result: func(r *onboard.Result) {
		r.PullRequests = onboard.PullRequests{OK: false, Error: "the GitHub CLI is not signed in"}
	}})

	code, out, errOut := runCLI("project", "add", "acme/demo", "--no-checks", "--yes", "--config", path)
	assert.Equal(t, 1, code, errOut)
	assert.Contains(t, out, "  STOP  pull requests   the GitHub CLI is not signed in on this machine. Run gh auth login, then try again.\n")
	assert.Contains(t, out, "\nNothing was added.\n")
	assert.Equal(t, before, readAddFile(t, path))
}

func TestAddLegacyNameWithRepoClaimsTheCopyUnderThatName(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	f := useFake(t, &fakeOnboard{})

	code, _, errOut := runCLI("project", "add", "mine", "--repo", "acme/demo", "--no-checks", "--yes", "--config", path)
	require.Equal(t, 0, code, errOut)
	require.Len(t, f.claimed, 1)
	assert.Equal(t, "acme/demo", f.claimed[0][1])
	assert.Equal(t, "mine", f.claimed[0][2])
	assert.Equal(t, "acme/demo", parseAddFile(t, path).Projects["mine"].Repo)
}

func TestAddReloadFailureSaysNotRunning(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	useFake(t, &fakeOnboard{})
	useReloadErr(t, errors.New("connection refused"))

	code, out, errOut := runCLI("project", "add", "acme/demo", "--no-checks", "--yes", "--config", path)
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "Added demo to "+path+".\nonett is not running; it will read the new project when it starts.\n")
	assert.NotContains(t, out, "reloaded onett")
}

func TestAddTerminalPicksARepositoryFromTheList(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	now := time.Now()
	owned := "app"
	f := useFake(t, &fakeOnboard{
		result: guessedCommands,
		repos: onboard.ReposResult{
			GH: onboard.GHStatus{SignedIn: true, User: "bobbyrc"},
			Repos: []onboard.GitHubRepo{
				{FullName: "bobbyrc/pimlo", Private: true, PushedAt: now.Add(-2 * time.Hour)},
				{FullName: "bobbyrc/app", Private: true, PushedAt: now.Add(-3 * time.Hour), Project: &owned},
				{FullName: "bobbyrc/home-lab", Private: true, PushedAt: now.Add(-26 * time.Hour)},
			},
		},
	})
	var out, errOut bytes.Buffer
	e := &env{out: &out, errOut: &errOut, in: strings.NewReader("2\ny\n\n"), isTTY: true}

	require.NoError(t, addProject(e, []string{"--config", path}))
	assert.Contains(t, out.String(), "Your repositories, most recently pushed first (GitHub CLI signed in as bobbyrc):\n")
	assert.Contains(t, out.String(), "  1  bobbyrc/pimlo")
	assert.Contains(t, out.String(), "  2  bobbyrc/home-lab")
	assert.NotContains(t, out.String(), "bobbyrc/app", "a repository that already has a project is not listed")
	assert.Contains(t, out.String(), "Checking bobbyrc/home-lab\n")
	assert.Equal(t, []string{"bobbyrc/home-lab"}, f.inspected)

	p := parseAddFile(t, path).Projects["home-lab"]
	assert.Equal(t, "bobbyrc/home-lab", p.Repo)
	require.Len(t, p.Setup, 1)
	assert.Contains(t, out.String(), "Added home-lab to "+path+" and reloaded onett.\n")
}

func TestAddTerminalDeclineWritesNothing(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	before := readAddFile(t, path)
	f := useFake(t, &fakeOnboard{})
	var out, errOut bytes.Buffer
	e := &env{out: &out, errOut: &errOut, in: strings.NewReader("n\n"), isTTY: true}

	require.NoError(t, addProject(e, []string{"acme/demo", "--no-checks", "--config", path}))
	assert.Contains(t, out.String(), "Add demo? [Y/n]: ")
	assert.Contains(t, out.String(), "Nothing was added.\n")
	assert.Equal(t, []string{"demo"}, f.discarded)
	assert.Equal(t, before, readAddFile(t, path))
}

func TestAddTerminalWithGHSignedOutAsksForAName(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	before := readAddFile(t, path)
	useFake(t, &fakeOnboard{repos: onboard.ReposResult{GH: onboard.GHStatus{SignedIn: false, Fix: "gh auth login"}}})
	var out, errOut bytes.Buffer
	e := &env{out: &out, errOut: &errOut, in: strings.NewReader("\n"), isTTY: true}

	require.NoError(t, addProject(e, []string{"--config", path}))
	assert.Contains(t, out.String(), "The GitHub CLI is not signed in on this machine.")
	assert.Contains(t, out.String(), "  gh auth login\n")
	assert.Contains(t, out.String(), "Repository (owner/name, a GitHub link, or a local path): ")
	assert.Contains(t, out.String(), "Nothing was added.")
	assert.Equal(t, before, readAddFile(t, path))
}

func TestAddTerminalEditsTheGuess(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	useFake(t, &fakeOnboard{result: guessedCommands})
	var out, errOut bytes.Buffer
	// e: edit. setup npm ci kept; npm run lint removed; npm test replaced; no more setup; one
	// more check; then Enter ends the checks; y adds the project.
	answers := "e\n\n-\nmake test\n\nmake check\n\ny\n"
	e := &env{out: &out, errOut: &errOut, in: strings.NewReader(answers), isTTY: true}

	require.NoError(t, addProject(e, []string{"acme/pimlo", "--config", path}))
	p := parseAddFile(t, path).Projects["pimlo"]
	require.Len(t, p.Setup, 1)
	assert.Equal(t, "npm ci", p.Setup[0].Run)
	require.Len(t, p.Checks, 2)
	assert.Equal(t, "make test", p.Checks[0].Run)
	assert.Equal(t, "make check", p.Checks[1].Run)
}

func TestAgeWords(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"just pushed", now.Add(-5 * time.Minute), "just now"},
		{"hours", now.Add(-2 * time.Hour), "2h ago"},
		{"yesterday", now.Add(-26 * time.Hour), "yesterday"},
		{"days", now.Add(-3 * 24 * time.Hour), "3d ago"},
		{"weeks", now.Add(-14 * 24 * time.Hour), "2w ago"},
		{"a month", now.Add(-30 * 24 * time.Hour), "1mo ago"},
		{"months", now.Add(-45 * 24 * time.Hour), "1mo ago"},
		{"years", now.Add(-400 * 24 * time.Hour), "1y ago"},
		{"never pushed", time.Time{}, "-"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, ageWords(now, c.at))
		})
	}
}
