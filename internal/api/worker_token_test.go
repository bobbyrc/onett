package api

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/auth"
)

var workerTok = auth.WorkerToken(testToken)

// workerReadable is every route the worker token may call. A route not listed here must refuse
// it; adding to this list widens what a worker agent can see.
var workerReadable = map[string]bool{
	"GET /api/v1/tasks":       true,
	"GET /api/v1/tasks/{id}":  true,
	"GET /api/v1/goals":       true,
	"GET /api/v1/goals/{id}":  true,
	"GET /api/v1/deployments": true,
}

func TestWorkerTokenReadsOnlyTasksGoalsAndDeployments(t *testing.T) {
	x := newHarness(t)
	routes, ok := x.h.(chi.Routes)
	require.True(t, ok)
	param := regexp.MustCompile(`\{[^}]*\}`)

	checked := 0
	require.NoError(t, chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/v1/") || route == "/api/v1/health" || strings.HasPrefix(route, "/api/v1/auth/") {
			return nil
		}
		checked++
		rec := x.authReq(t, method, param.ReplaceAllString(route, "x"), "", withToken(workerTok))
		key := method + " " + chiParam.ReplaceAllString(route, "{$1}")
		if workerReadable[key] {
			assertNotDenied(t, rec, "worker token on "+key)
		} else {
			assert.Equal(t, http.StatusForbidden, rec.Code, "worker token on %s: %s", key, rec.Body.String())
		}
		return nil
	}))
	assert.Greater(t, checked, 20)
}

func TestWorkerTokenIsNeitherTheOwnerNorTheAgentToken(t *testing.T) {
	assert.NotEqual(t, testToken, workerTok)
	assert.NotEqual(t, agentTok, workerTok)
	assert.Empty(t, auth.WorkerToken(""), "an empty owner token gives an empty worker token")
}

func TestWorkerTokenIsRefusedAfterNoTokenConfigured(t *testing.T) {
	x := newHarness(t)
	rec := x.authReq(t, http.MethodGet, "/api/v1/tasks", "", withToken("not-the-token"))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
