package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/store/sqlite"
)

// secretsHarness is newHarness with a secrets file under a temp directory.
func secretsHarness(t *testing.T) (*harness, string) {
	t.Helper()
	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := events.NewBus(st, logger)
	ctl := &fakeCtl{cfg: testConfig(), errs: map[string]error{}}
	path := filepath.Join(t.TempDir(), "secrets.env")
	h := NewServer(Options{
		Store: st, Bus: bus, Controller: ctl, Token: testToken, Version: "test-version",
		SecretsPath: path, Web: fstest.MapFS{}, Logger: logger,
	})
	return &harness{h: h, st: st, bus: bus, ctl: ctl}, path
}

func TestOwnerSetsListsAndRemovesSecrets(t *testing.T) {
	x, path := secretsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	const name = "ONETT_TEST_API_SECRET"
	t.Setenv(name, "") // registers the restore; the handler sets the real value
	os.Unsetenv(name)

	rec := x.authReq(t, http.MethodPut, "/api/v1/secrets/"+name, `{"value":"s3cr3t-value"}`, withCookie(c))
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, "s3cr3t-value", os.Getenv(name), "applies to the running process")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, name+"=s3cr3t-value\n", string(data))

	rec = x.authReq(t, http.MethodGet, "/api/v1/secrets", "", withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code)
	want, _ := json.Marshal(secretList{Path: path, Names: []string{name}})
	assert.JSONEq(t, string(want), rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "s3cr3t-value", "values are never returned")

	rec = x.authReq(t, http.MethodDelete, "/api/v1/secrets/"+name, "", withCookie(c))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	rec = x.authReq(t, http.MethodDelete, "/api/v1/secrets/"+name, "", withCookie(c))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSecretsRefuseBadInput(t *testing.T) {
	x, _ := secretsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)

	rec := x.authReq(t, http.MethodPut, "/api/v1/secrets/1BAD", `{"value":"x"}`, withCookie(c))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	rec = x.authReq(t, http.MethodPut, "/api/v1/secrets/OK_NAME", `{"value":""}`, withCookie(c))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	rec = x.authReq(t, http.MethodPut, "/api/v1/secrets/OK_NAME", `{"value":"x","extra":1}`, withCookie(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code, "unknown fields are errors")
}

func TestSecretsAreOwnerOnly(t *testing.T) {
	x, _ := secretsHarness(t)
	ops := x.newUserSession(t, "ops", domain.RoleOperator)

	for _, req := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/secrets", ""},
		{http.MethodPut, "/api/v1/secrets/A", `{"value":"x"}`},
		{http.MethodDelete, "/api/v1/secrets/A", ""},
	} {
		rec := x.authReq(t, req.method, req.path, req.body, withCookie(ops))
		assert.Equal(t, http.StatusForbidden, rec.Code, req.method+" "+req.path)
		rec = x.authReq(t, req.method, req.path, req.body, withToken(testToken))
		assert.Equal(t, http.StatusForbidden, rec.Code, "the API token must not reach secrets: "+req.method)
	}
}

func TestSecretsRoutesOffWithoutPath(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	rec := x.authReq(t, http.MethodGet, "/api/v1/secrets", "", withCookie(c))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
