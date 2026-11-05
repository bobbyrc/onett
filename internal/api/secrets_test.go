package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/secrets"
)

// secretsHarness is a configHarness whose config refers to two secrets, with a secrets store in
// a temp dir and every log line captured.
func secretsHarness(t *testing.T) (*harness, string, *bytes.Buffer) {
	t.Helper()
	x := newHarness(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "onett.yaml")
	text := "server:\n  data_dir: " + dir + "\nchannels:\n  - type: telegram\n    options:\n      bot_token_env: ONETT_TEST_TG\n      allowed_user_ids: [1]\nnotify:\n  - type: discord\n    options:\n      webhook_url: ${ONETT_TEST_HOOK}\n"
	t.Setenv("ONETT_TEST_HOOK", "placeholder")
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	cfg, err := config.Parse([]byte(text), path)
	require.NoError(t, err)
	x.ctl.cfg = cfg

	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	secretsPath := filepath.Join(dir, "cfg", "secrets.env")
	x.h = NewServer(Options{
		Store: x.st, Bus: events.NewBus(x.st, logger), Controller: x.ctl, Token: testToken,
		Version: "test", Web: fstest.MapFS{}, Logger: logger, Secrets: secrets.New(secretsPath),
	})
	t.Cleanup(func() { _ = os.Unsetenv("ONETT_TEST_TG") })
	return x, secretsPath, logs
}

type secretList struct {
	Secrets []secretView `json:"secrets"`
}

func TestSecretsListUnionOfFileAndConfigReferences(t *testing.T) {
	x, path, _ := secretsHarness(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("ONETT_TEST_EXTRA=zzz\n"), 0o600))
	t.Cleanup(func() { _ = os.Unsetenv("ONETT_TEST_EXTRA") })
	c := x.newUserSession(t, "boss", domain.RoleOwner)

	rec := x.authReq(t, http.MethodGet, "/api/v1/secrets", "", withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got secretList
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	byName := map[string]secretView{}
	for _, s := range got.Secrets {
		byName[s.Name] = s
	}
	assert.Equal(t, secretView{Name: "ONETT_TEST_TG", ReferencedBy: []string{"channels[0].options.bot_token_env"}}, byName["ONETT_TEST_TG"])
	assert.Equal(t, secretView{Name: "ONETT_TEST_HOOK", Set: true, Source: "environment", ReferencedBy: []string{"notify[0].options.webhook_url"}}, byName["ONETT_TEST_HOOK"])
	assert.Equal(t, secretView{Name: "ONETT_TEST_EXTRA", ReferencedBy: []string{}}, byName["ONETT_TEST_EXTRA"], "in the file but never loaded: not set")
	assert.NotContains(t, rec.Body.String(), "zzz")
	assert.NotContains(t, rec.Body.String(), "placeholder")
}

func TestSecretsSetNeverLeaksTheValue(t *testing.T) {
	x, path, logs := secretsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	const value = "s3cr3t-token-VALUE-12345"

	var bodies []string
	do := func(method, target, body string) *httptest.ResponseRecorder {
		rec := x.authReq(t, method, target, body, withCookie(c))
		bodies = append(bodies, rec.Body.String())
		return rec
	}

	rec := do(http.MethodPut, "/api/v1/secrets/ONETT_TEST_TG", `{"value":"`+value+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"name":"ONETT_TEST_TG","set":true,"shadowed":false}`, rec.Body.String())
	assert.Equal(t, 1, x.ctl.reloadCalls, "the config is reloaded")
	assert.Equal(t, value, os.Getenv("ONETT_TEST_TG"))

	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	file, _ := os.ReadFile(path)
	assert.Equal(t, "ONETT_TEST_TG="+value+"\n", string(file))

	rec = do(http.MethodGet, "/api/v1/secrets", "")
	assert.Contains(t, rec.Body.String(), `"source":"file"`)
	do(http.MethodGet, "/api/v1/config", "")
	do(http.MethodGet, "/api/v1/config/file", "")
	do(http.MethodGet, "/api/v1/config/document", "")
	// A bad request must not echo the body either.
	do(http.MethodPut, "/api/v1/secrets/ONETT_TEST_TG", `{"value":"`+value+`","extra":1}`)
	do(http.MethodPut, "/api/v1/secrets/lower", `{"value":"`+value+`"}`)

	for _, b := range bodies {
		assert.NotContains(t, b, value)
	}
	assert.NotContains(t, logs.String(), value, "logs never hold a value")

	rec = do(http.MethodDelete, "/api/v1/secrets/ONETT_TEST_TG", "")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, 2, x.ctl.reloadCalls)
	_, set := os.LookupEnv("ONETT_TEST_TG")
	assert.False(t, set)
	assert.Equal(t, http.StatusNotFound, do(http.MethodDelete, "/api/v1/secrets/ONETT_TEST_TG", "").Code)
	assert.NotContains(t, logs.String(), value)
}

func TestSecretsValidation(t *testing.T) {
	x, _, _ := secretsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	for _, tc := range []struct{ name, body string }{
		{"lower", `{"value":"x"}`},
		{"1ABC", `{"value":"x"}`},
		{"ONETT_TEST_TG", `{"value":""}`},
		{"ONETT_TEST_TG", `{}`},
		{"ONETT_TEST_TG", `{"value":"a\nb"}`},
	} {
		rec := x.authReq(t, http.MethodPut, "/api/v1/secrets/"+tc.name, tc.body, withCookie(c))
		assert.Equal(t, http.StatusBadRequest, rec.Code, tc.name+" "+tc.body)
	}
	assert.Zero(t, x.ctl.reloadCalls)
}

func TestSecretsShadowedByRealEnvironment(t *testing.T) {
	x, _, _ := secretsHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	rec := x.authReq(t, http.MethodPut, "/api/v1/secrets/ONETT_TEST_HOOK", `{"value":"new"}`, withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"name":"ONETT_TEST_HOOK","set":true,"shadowed":true}`, rec.Body.String())
	assert.Equal(t, "placeholder", os.Getenv("ONETT_TEST_HOOK"))
}

func TestSecretsAreOwnerOnly(t *testing.T) {
	x, path, _ := secretsHarness(t)
	for _, tc := range []struct{ method, target, body string }{
		{http.MethodGet, "/api/v1/secrets", ""},
		{http.MethodPut, "/api/v1/secrets/ONETT_TEST_TG", `{"value":"x"}`},
		{http.MethodDelete, "/api/v1/secrets/ONETT_TEST_TG", ""},
	} {
		rec := x.authReq(t, tc.method, tc.target, tc.body, withToken(testToken))
		assert.Equal(t, http.StatusForbidden, rec.Code, "API token: "+tc.method+" "+tc.target)
	}
	op := x.newUserSession(t, "oper", domain.RoleOperator)
	rec := x.authReq(t, http.MethodPut, "/api/v1/secrets/ONETT_TEST_TG", `{"value":"x"}`, withCookie(op))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = x.authReq(t, http.MethodGet, "/api/v1/secrets", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "nothing was written")
}

func TestSecretsWithoutStoreIsNotImplemented(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	rec := x.authReq(t, http.MethodGet, "/api/v1/secrets", "", withCookie(c))
	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}
