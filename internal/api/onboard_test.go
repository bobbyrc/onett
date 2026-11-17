package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/auth"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/onboard"
	"github.com/bobbyrc/onett/internal/workspace"
)

// signedOutGH is the GitHub CLI for these tests: it fails every call, as a machine where gh is
// not signed in does, so no test can reach GitHub.
func signedOutGH(context.Context, ...string) ([]byte, error) {
	return nil, errors.New("gh: not signed in (test stub)")
}

// enableOnboard gives x's server an onboard.Service that copies under the data_dir of its config,
// as serve does. newHarness leaves Options.Onboard nil, so the add-project routes answer 501.
func (x *harness) enableOnboard(t *testing.T) {
	t.Helper()
	svc := onboard.New(workspace.New(filepath.Join(x.ctl.cfg.Server.DataDir, "worktrees")), signedOutGH)
	x.h = NewServer(Options{
		Store:      x.st,
		Bus:        x.bus,
		Controller: x.ctl,
		Token:      testToken,
		Version:    "test-version",
		Web:        fstest.MapFS{},
		Restart:    func() { x.restarts++ },
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Onboard:    svc,
	})
}

// bareRepo makes a local bare repository with one commit on master and returns its absolute
// path. Tests pass that path as the repository, so git never leaves the machine.
func bareRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "no-such-gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	gitIn(t, root, "init", "-b", "master", seed)
	require.NoError(t, os.WriteFile(filepath.Join(seed, "README.md"), []byte("hello\n"), 0o644))
	gitIn(t, seed, "add", "-A")
	gitIn(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", "init")
	bare := filepath.Join(root, "project.git")
	gitIn(t, root, "clone", "--bare", "--quiet", seed, bare)
	return bare
}

// gitIn runs git in dir for test setup.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// jsonBody encodes v as a request body.
func jsonBody(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// inspectRepo asks the inspect route to check repo and requires that it streamed a result.
func inspectRepo(t *testing.T, x *harness, c, repo string) {
	t.Helper()
	rec := x.authReq(t, http.MethodPost, "/api/v1/projects/inspect", jsonBody(t, map[string]string{"repo": repo}), withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// ndjsonLines splits a streamed answer into one decoded object per line.
func ndjsonLines(t *testing.T, rec *httptest.ResponseRecorder) []map[string]json.RawMessage {
	t.Helper()
	var out []map[string]json.RawMessage
	for _, ln := range strings.Split(strings.TrimSpace(rec.Body.String()), "\n") {
		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(ln), &m), "line: %s", ln)
		out = append(out, m)
	}
	return out
}

func TestOnboardRoutesAnswer501WithoutService(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/github/repos", ""},
		{http.MethodPost, "/api/v1/projects/inspect", `{"repo":"acme/app"}`},
		{http.MethodDelete, "/api/v1/projects/inspect/app", ""},
		{http.MethodPost, "/api/v1/projects", `{"repo":"acme/app"}`},
	}
	for _, rt := range routes {
		rec := x.authReq(t, rt.method, rt.path, rt.body, withCookie(c))
		assert.Equal(t, http.StatusNotImplemented, rec.Code, "%s %s: %s", rt.method, rt.path, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "not set up")
	}
}

func TestOnboardRoutesAreOwnerSessionOnly(t *testing.T) {
	x, _, _ := projectsHarness(t)
	agent := auth.AgentToken(testToken)
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/github/repos", ""},
		{http.MethodPost, "/api/v1/projects/inspect", `{"repo":"acme/app"}`},
		{http.MethodDelete, "/api/v1/projects/inspect/app", ""},
		{http.MethodPost, "/api/v1/projects", `{"repo":"acme/web"}`},
	}
	for _, rt := range routes {
		for _, tok := range []string{testToken, agent} {
			rec := x.authReq(t, rt.method, rt.path, rt.body, withToken(tok))
			assert.Equal(t, http.StatusForbidden, rec.Code, "%s %s: %s", rt.method, rt.path, rec.Body.String())
		}
	}
}

func TestInspectStreamsStepsThenResult(t *testing.T) {
	x, _, _ := projectsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	repo := bareRepo(t)

	rec := x.authReq(t, http.MethodPost, "/api/v1/projects/inspect", jsonBody(t, map[string]string{"repo": repo}), withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "application/x-ndjson", rec.Header().Get("Content-Type"))

	lines := ndjsonLines(t, rec)
	require.Greater(t, len(lines), 2)
	var steps []string
	for _, ln := range lines[:len(lines)-1] {
		var st onboard.Step
		require.NoError(t, json.Unmarshal(ln["step"], &st), "every line before the last is a step")
		steps = append(steps, st.Name+":"+st.State)
	}
	assert.Equal(t, []string{
		"copy:running", "copy:done", "branch:running", "branch:done",
		"pull_requests:running", "pull_requests:skipped", "guess:running", "guess:done",
	}, steps)

	last := lines[len(lines)-1]
	require.Contains(t, last, "result", "the last line is the result")
	var res onboard.Result
	require.NoError(t, json.Unmarshal(last["result"], &res))
	assert.True(t, res.Copy.OK)
	assert.Equal(t, "master", res.DefaultBranch)
	assert.False(t, res.GitHub)
	assert.Equal(t, repo, res.Repo)
	assert.DirExists(t, filepath.Join(x.ctl.cfg.Server.DataDir, "repos", "project"))
}

func TestInspectRefusalsAreJSONErrorsNotStreams(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
	}{
		{"empty repo", `{"repo":"  "}`, http.StatusBadRequest},
		{"unknown field", `{"repo":"acme/web","bogus":1}`, http.StatusBadRequest},
		{"not a repo", `{"repo":"not a repo"}`, http.StatusBadRequest},
		{"name already configured", `{"repo":"acme/app"}`, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x, _, _ := projectsHarness(t)
			c := x.newUserSession(t, "boss", domain.RoleOwner)
			rec := x.authReq(t, http.MethodPost, "/api/v1/projects/inspect", tc.body, withCookie(c))
			assert.Equal(t, tc.status, rec.Code, rec.Body.String())
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			var out struct {
				Error string `json:"error"`
			}
			decode(t, rec, &out)
			assert.NotEmpty(t, out.Error)
		})
	}
}

func TestCreateProjectNeedsInspectFirst(t *testing.T) {
	x, path, text := projectsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	rec := x.authReq(t, http.MethodPost, "/api/v1/projects", jsonBody(t, map[string]string{"repo": bareRepo(t)}), withCookie(c))
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "POST /projects/inspect")
	got, _ := os.ReadFile(path)
	assert.Equal(t, text, string(got))
	assert.Zero(t, x.ctl.reloadCalls)
}

func TestCreateProjectAfterInspectWritesSupervisedProject(t *testing.T) {
	x, path, _ := projectsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	repo := bareRepo(t)
	inspectRepo(t, x, c, repo)

	rec := x.authReq(t, http.MethodPost, "/api/v1/projects", jsonBody(t, map[string]any{
		"repo":   repo,
		"setup":  []map[string]string{{"name": "npm ci", "run": "npm ci"}},
		"checks": []map[string]string{{"name": "go test", "run": "go test ./..."}},
	}), withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	got, _ := os.ReadFile(path)
	cfg, err := config.Parse(got, path)
	require.NoError(t, err)
	p := cfg.Projects["project"]
	require.NotNil(t, p)
	assert.Equal(t, "master", p.DefaultBranch)
	assert.Equal(t, "supervised", p.Policy)
	require.Len(t, p.Setup, 1)
	assert.Equal(t, "npm ci", p.Setup[0].Name)
	assert.Equal(t, "npm ci", p.Setup[0].Run)
	require.Len(t, p.Checks, 1)
	assert.Equal(t, "go test", p.Checks[0].Name)
	assert.Equal(t, "go test ./...", p.Checks[0].Run)
	assert.DirExists(t, filepath.Join(x.ctl.cfg.Server.DataDir, "repos", "project"), "the checked copy becomes the project's")
}

func TestCreateProjectRenamesTheCopy(t *testing.T) {
	x, _, _ := projectsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	repo := bareRepo(t)
	inspectRepo(t, x, c, repo)
	dataDir := x.ctl.cfg.Server.DataDir

	rec := x.authReq(t, http.MethodPost, "/api/v1/projects", jsonBody(t, map[string]string{"repo": repo, "name": "web"}), withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NoDirExists(t, filepath.Join(dataDir, "repos", "project"))
	assert.DirExists(t, filepath.Join(dataDir, "repos", "web"))
}

func TestCreateProjectRefusesARenameOntoAnExistingCopy(t *testing.T) {
	x, path, text := projectsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	repo := bareRepo(t)
	inspectRepo(t, x, c, repo)
	taken := filepath.Join(x.ctl.cfg.Server.DataDir, "repos", "web")
	require.NoError(t, os.MkdirAll(taken, 0o755))

	rec := x.authReq(t, http.MethodPost, "/api/v1/projects", jsonBody(t, map[string]string{"repo": repo, "name": "web"}), withCookie(c))
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	got, _ := os.ReadFile(path)
	assert.Equal(t, text, string(got))
	assert.DirExists(t, taken)
}

func TestCreateProjectRejectsPathAndUnknownFields(t *testing.T) {
	x, path, text := projectsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	repo := bareRepo(t)
	inspectRepo(t, x, c, repo)

	rec := x.authReq(t, http.MethodPost, "/api/v1/projects", jsonBody(t, map[string]string{"repo": repo, "path": "/elsewhere"}), withCookie(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	got, _ := os.ReadFile(path)
	assert.Equal(t, text, string(got))
	assert.Zero(t, x.ctl.reloadCalls)
}

func TestDiscardInspectRemovesTheCopy(t *testing.T) {
	x, _, _ := projectsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	inspectRepo(t, x, c, bareRepo(t))
	copyDir := filepath.Join(x.ctl.cfg.Server.DataDir, "repos", "project")
	require.DirExists(t, copyDir)

	rec := x.authReq(t, http.MethodDelete, "/api/v1/projects/inspect/project", "", withCookie(c))
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.NoDirExists(t, copyDir)

	rec = x.authReq(t, http.MethodDelete, "/api/v1/projects/inspect/project", "", withCookie(c))
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestDiscardConfiguredProjectIsConflict(t *testing.T) {
	x, _, _ := projectsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	rec := x.authReq(t, http.MethodDelete, "/api/v1/projects/inspect/app", "", withCookie(c))
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}

func TestListGithubReposSignedOut(t *testing.T) {
	x, _, _ := projectsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	rec := x.authReq(t, http.MethodGet, "/api/v1/github/repos", "", withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"repos":[]`, "an empty list is [], not null")

	var out onboard.ReposResult
	decode(t, rec, &out)
	assert.False(t, out.GH.SignedIn)
	assert.Equal(t, "gh auth login", out.GH.Fix)
	assert.Empty(t, out.Repos)
}
