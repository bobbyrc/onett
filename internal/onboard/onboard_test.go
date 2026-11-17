package onboard

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/workspace"
)

// TestMain keeps git to local paths: no test here may reach a real host.
func TestMain(m *testing.M) {
	os.Setenv("GIT_ALLOW_PROTOCOL", "file")
	os.Exit(m.Run())
}

// fakeGH answers the gh calls a test expects, keyed by the first two arguments ("repo list").
// Any other call fails and is recorded, so a test can check that no gh call happened.
type fakeGH struct {
	signedIn bool
	answers  map[string]string
	calls    []string
}

func (f *fakeGH) run(_ context.Context, args ...string) ([]byte, error) {
	key := strings.Join(args[:min(2, len(args))], " ")
	f.calls = append(f.calls, key)
	if key == "auth status" {
		if !f.signedIn {
			return nil, errors.New("gh auth status: You are not logged into any GitHub hosts")
		}
		return []byte("Logged in"), nil
	}
	if out, ok := f.answers[key]; ok {
		return []byte(out), nil
	}
	return nil, errors.New("gh " + key + ": no canned answer")
}

// onboardGit runs git in dir for test setup, failing the test on error.
func onboardGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// localBareRepo makes a local bare repository (HEAD names master, one commit) and returns its
// absolute path, which is the repo a test passes to Inspect.
func localBareRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "no-such-gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	onboardGit(t, root, "init", "-b", "master", seed)
	require.NoError(t, os.WriteFile(filepath.Join(seed, "README.md"), []byte("hello\n"), 0o644))
	onboardGit(t, seed, "add", "-A")
	onboardGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", "init")
	bare := filepath.Join(root, "project.git")
	onboardGit(t, root, "clone", "--bare", "--quiet", seed, bare)
	return bare
}

// newTestService returns a Service copying into a fresh temp dir, with gh answering from f.
func newTestService(t *testing.T, f *fakeGH) (*Service, string) {
	t.Helper()
	dataDir := t.TempDir()
	return New(workspace.New(filepath.Join(dataDir, "worktrees")), f.run), dataDir
}

func TestInspectLocalRepoIsReady(t *testing.T) {
	bare := localBareRepo(t)
	f := &fakeGH{}
	svc, dataDir := newTestService(t, f)

	var steps []Step
	res, err := svc.Inspect(context.Background(), Request{Repo: bare, DataDir: dataDir},
		func(s Step) { steps = append(steps, s) })
	require.NoError(t, err)

	assert.True(t, res.Copy.OK, res.Copy.Error)
	assert.Equal(t, "project", res.Name)
	assert.Equal(t, bare, res.Repo)
	assert.False(t, res.GitHub)
	assert.Equal(t, "master", res.DefaultBranch)
	assert.Equal(t, "repository", res.BranchFrom)
	assert.False(t, res.PullRequests.OK)
	assert.True(t, res.Ready())
	assert.Empty(t, f.calls, "a local repository never asks gh")

	assert.DirExists(t, filepath.Join(dataDir, "repos", "project", ".git"))

	var seen []string
	for _, s := range steps {
		seen = append(seen, s.Name+" "+s.State)
	}
	assert.Equal(t, []string{
		"copy running", "copy done",
		"branch running", "branch done",
		"pull_requests running", "pull_requests skipped",
		"guess running", "guess done",
	}, seen)

	checked, ok := svc.Checked(bare)
	require.True(t, ok)
	assert.Equal(t, res, checked)
}

func TestInspectMissingPathIsNotReady(t *testing.T) {
	f := &fakeGH{}
	svc, dataDir := newTestService(t, f)
	missing := filepath.Join(t.TempDir(), "missing.git")

	var steps []Step
	res, err := svc.Inspect(context.Background(), Request{Repo: missing, DataDir: dataDir},
		func(s Step) { steps = append(steps, s) })
	require.NoError(t, err)
	assert.False(t, res.Copy.OK)
	assert.NotEmpty(t, res.Copy.Error)
	assert.False(t, res.Ready())
	require.Len(t, steps, 5, "copy running, copy failed, then branch, pull_requests and guess skipped")
	assert.Equal(t, Step{Name: "copy", State: "failed", Detail: res.Copy.Error}, steps[1])
	assert.Equal(t, "branch", steps[2].Name)
	assert.Equal(t, "skipped", steps[2].State)
	_, ok := svc.Checked(missing)
	assert.False(t, ok)
}

func TestInspectRejectsNameAlreadyConfigured(t *testing.T) {
	svc, dataDir := newTestService(t, &fakeGH{})
	_, err := svc.Inspect(context.Background(), Request{
		Repo: localBareRepo(t), DataDir: dataDir, Existing: map[string]string{"project": "/elsewhere"},
	}, nil)
	assert.ErrorIs(t, err, ErrExists)
	assert.NoDirExists(t, filepath.Join(dataDir, "repos", "project"))
}

func TestInspectRejectsRepoAlreadyConfigured(t *testing.T) {
	bare := localBareRepo(t)
	svc, dataDir := newTestService(t, &fakeGH{})
	_, err := svc.Inspect(context.Background(), Request{
		Repo: bare, DataDir: dataDir, Existing: map[string]string{"other": bare},
	}, nil)
	assert.ErrorIs(t, err, ErrExists)
}

func TestInspectRejectsBadRepoBeforeWork(t *testing.T) {
	svc, dataDir := newTestService(t, &fakeGH{})
	_, err := svc.Inspect(context.Background(), Request{Repo: "foo", DataDir: dataDir}, nil)
	assert.ErrorIs(t, err, ErrInvalid)
	assert.NoDirExists(t, filepath.Join(dataDir, "repos"))
}

func TestInspectRejectsSecondInspectOfSameName(t *testing.T) {
	svc, dataDir := newTestService(t, &fakeGH{})
	svc.busy["project"] = true
	_, err := svc.Inspect(context.Background(), Request{Repo: localBareRepo(t), DataDir: dataDir}, nil)
	assert.ErrorIs(t, err, ErrBusy)
}

func TestDiscardRemovesOwnCopy(t *testing.T) {
	bare := localBareRepo(t)
	svc, dataDir := newTestService(t, &fakeGH{})
	_, err := svc.Inspect(context.Background(), Request{Repo: bare, DataDir: dataDir}, nil)
	require.NoError(t, err)
	copyPath := filepath.Join(dataDir, "repos", "project")

	require.ErrorIs(t, svc.Discard("project", map[string]string{"project": bare}), ErrInUse)
	require.DirExists(t, copyPath)

	require.NoError(t, svc.Discard("project", nil))
	assert.NoDirExists(t, copyPath)
	assert.ErrorIs(t, svc.Discard("project", nil), ErrNotFound)
}

func TestClaimRenamesAndForgets(t *testing.T) {
	bare := localBareRepo(t)
	svc, dataDir := newTestService(t, &fakeGH{})

	assert.ErrorIs(t, svc.Claim(dataDir, bare, "pimlo"), ErrNotChecked)

	_, err := svc.Inspect(context.Background(), Request{Repo: bare, DataDir: dataDir}, nil)
	require.NoError(t, err)

	require.NoError(t, svc.Claim(dataDir, bare, "pimlo"))
	assert.DirExists(t, filepath.Join(dataDir, "repos", "pimlo", ".git"))
	assert.NoDirExists(t, filepath.Join(dataDir, "repos", "project"))

	_, ok := svc.Checked(bare)
	assert.False(t, ok, "a claimed copy is no longer a pending check")
	assert.ErrorIs(t, svc.Discard("pimlo", nil), ErrNotFound, "the project owns the copy now")
}

func TestReposSortsByPushedAtAndMarksProject(t *testing.T) {
	f := &fakeGH{signedIn: true, answers: map[string]string{
		"api user": "bcraig\n",
		"repo list": `[
			{"nameWithOwner":"acme/old","isPrivate":false,"description":"","defaultBranchRef":{"name":"main"},"pushedAt":"2026-01-02T00:00:00Z"},
			{"nameWithOwner":"acme/pimlo","isPrivate":true,"description":"the app","defaultBranchRef":{"name":"main"},"pushedAt":"2026-03-01T00:00:00Z"},
			{"nameWithOwner":"acme/new","isPrivate":false,"description":"","defaultBranchRef":{"name":"trunk"},"pushedAt":"2026-06-01T00:00:00Z"}
		]`,
	}}
	svc, _ := newTestService(t, f)

	out := svc.Repos(context.Background(), map[string]string{"pimlo": "acme/pimlo"})
	assert.True(t, out.GH.SignedIn)
	assert.Equal(t, "bcraig", out.GH.User)
	assert.Empty(t, out.GH.Fix)
	require.Len(t, out.Repos, 3)
	assert.Equal(t, "acme/new", out.Repos[0].FullName)
	assert.Equal(t, "trunk", out.Repos[0].DefaultBranch)
	assert.Equal(t, "acme/pimlo", out.Repos[1].FullName)
	require.NotNil(t, out.Repos[1].Project)
	assert.Equal(t, "pimlo", *out.Repos[1].Project)
	assert.Nil(t, out.Repos[0].Project)
	assert.Equal(t, "acme/old", out.Repos[2].FullName)
}

func TestReposSignedOut(t *testing.T) {
	svc, _ := newTestService(t, &fakeGH{})
	out := svc.Repos(context.Background(), nil)
	assert.False(t, out.GH.SignedIn)
	assert.Equal(t, "gh auth login", out.GH.Fix)
	require.NotNil(t, out.Repos)
	assert.Empty(t, out.Repos)
}

func TestReposListFailureIsReported(t *testing.T) {
	svc, _ := newTestService(t, &fakeGH{signedIn: true, answers: map[string]string{"api user": "bcraig"}})
	out := svc.Repos(context.Background(), nil)
	assert.True(t, out.GH.SignedIn)
	assert.NotEmpty(t, out.GH.Error)
	require.NotNil(t, out.Repos)
	assert.Empty(t, out.Repos)
}
