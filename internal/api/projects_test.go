package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
)

// projectsHarness is configHarness with a config that already holds one project and comments
// around it, so tests can see what an add or a remove keeps.
func projectsHarness(t *testing.T) (*harness, string, string) {
	t.Helper()
	x := newHarness(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "onett.yaml")
	text := fmt.Sprintf(`# keep me
server:
  addr: 127.0.0.1:7777
  data_dir: %s

projects:
  # the first one
  app:
    repo: acme/app # trailing note

# tail comment
limits:
  max_attempts: 4
`, dir)
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	cfg, err := config.Parse([]byte(text), path)
	require.NoError(t, err)
	x.ctl.cfg = cfg
	return x, path, text
}

func TestCreateProjectInsertsKeepsCommentsAndReloads(t *testing.T) {
	x, path, _ := projectsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)

	rec := x.authReq(t, http.MethodPost, "/api/v1/projects",
		`{"name":"web","repo":"acme/web","branch":"develop","policy":"default","instructions":"Be brief.\nUse tabs."}`, withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct {
		Name    string `json:"name"`
		Repo    string `json:"repo"`
		Version string `json:"version"`
	}
	decode(t, rec, &out)
	assert.Equal(t, "web", out.Name)
	assert.Equal(t, "acme/web", out.Repo)
	assert.NotEmpty(t, out.Version)
	assert.Equal(t, 1, x.ctl.reloadCalls)

	got, _ := os.ReadFile(path)
	for _, keep := range []string{"# keep me", "# the first one", "# trailing note", "# tail comment"} {
		assert.Contains(t, string(got), keep)
	}
	cfg, err := config.Parse(got, path)
	require.NoError(t, err)
	p := cfg.Projects["web"]
	require.NotNil(t, p)
	assert.Equal(t, "develop", p.DefaultBranch)
	assert.Equal(t, "Be brief.\nUse tabs.", p.Instructions)
	assert.Equal(t, "acme/app", cfg.Projects["app"].Repo)
}

func TestCreateProjectRefusals(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
	}{
		{"duplicate name", `{"name":"app","repo":"acme/other"}`, http.StatusConflict},
		{"bad repo", `{"name":"web","repo":"not a repo"}`, http.StatusUnprocessableEntity},
		{"unknown policy", `{"name":"web","repo":"acme/web","policy":"nope"}`, http.StatusUnprocessableEntity},
		{"bad name", `{"name":"we b","repo":"acme/web"}`, http.StatusUnprocessableEntity},
		{"missing repo", `{"name":"web"}`, http.StatusBadRequest},
		{"unknown field", `{"name":"web","repo":"acme/web","bogus":1}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x, path, text := projectsHarness(t)
			c := x.newUserSession(t, "boss", domain.RoleOwner)
			rec := x.authReq(t, http.MethodPost, "/api/v1/projects", tc.body, withCookie(c))
			assert.Equal(t, tc.status, rec.Code, rec.Body.String())
			got, _ := os.ReadFile(path)
			assert.Equal(t, text, string(got), "a refused add writes nothing")
			assert.Zero(t, x.ctl.reloadCalls)
		})
	}
}

func TestCreateProjectReportsProblems(t *testing.T) {
	x, _, _ := projectsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	rec := x.authReq(t, http.MethodPost, "/api/v1/projects", `{"name":"web","repo":"acme/web","policy":"nope"}`, withCookie(c))
	var out struct {
		Problems []config.Problem `json:"problems"`
	}
	decode(t, rec, &out)
	require.NotEmpty(t, out.Problems)
	assert.Equal(t, "projects.web.policy", out.Problems[0].Path)
}

func TestProjectEndpointsAreOwnerOnly(t *testing.T) {
	x, _, _ := projectsHarness(t)
	rec := x.call(t, http.MethodPost, "/api/v1/projects", `{"name":"web","repo":"acme/web"}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, "the API token cannot edit config")
	rec = x.call(t, http.MethodDelete, "/api/v1/projects/app", "")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestDeleteProjectRemovesBlockKeepsComments(t *testing.T) {
	x, path, _ := projectsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)

	rec := x.authReq(t, http.MethodDelete, "/api/v1/projects/app", "", withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 1, x.ctl.reloadCalls)
	got, _ := os.ReadFile(path)
	assert.NotContains(t, string(got), "acme/app")
	assert.Contains(t, string(got), "# keep me")
	assert.Contains(t, string(got), "# tail comment")
	cfg, err := config.Parse(got, path)
	require.NoError(t, err)
	assert.Empty(t, cfg.Projects)
}

func TestDeleteProjectWithActiveTasksIsConflictUnlessForced(t *testing.T) {
	for _, st := range []domain.TaskStatus{domain.TaskQueued, domain.TaskRunning} {
		t.Run(string(st), func(t *testing.T) {
			x, path, text := projectsHarness(t)
			c := x.newUserSession(t, "boss", domain.RoleOwner)
			x.makeTask(t, "app", st, "")

			rec := x.authReq(t, http.MethodDelete, "/api/v1/projects/app", "", withCookie(c))
			assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
			got, _ := os.ReadFile(path)
			assert.Equal(t, text, string(got))

			rec = x.authReq(t, http.MethodDelete, "/api/v1/projects/app?force=true", "", withCookie(c))
			assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			got, _ = os.ReadFile(path)
			assert.NotContains(t, string(got), "acme/app")
		})
	}
}

func TestDeleteUnknownProjectIsNotFound(t *testing.T) {
	x, _, _ := projectsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	rec := x.authReq(t, http.MethodDelete, "/api/v1/projects/ghost", "", withCookie(c))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
