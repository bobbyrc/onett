package api

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/auth"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// agentTok is the token the orchestrator agent sends, derived from the harness's owner token.
var agentTok = auth.AgentToken(testToken)

// assertPersonOnly checks the agent token is answered 403 with the reason text.
func assertPersonOnly(t *testing.T, rec *httptest.ResponseRecorder, reason string) {
	t.Helper()
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), reason)
}

// assertNotDenied checks a request the caller may make is not refused for its identity.
func assertNotDenied(t *testing.T, rec *httptest.ResponseRecorder, who string) {
	t.Helper()
	assert.NotContains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, rec.Code,
		"%s: %s", who, rec.Body.String())
}

func TestAgentTokenCannotDecideApproval(t *testing.T) {
	x := newHarness(t)
	target := "/api/v1/approvals/some-id/decide"
	body := `{"approve":true}`
	cookie := x.newUserSession(t, "boss", domain.RoleOwner)

	assertPersonOnly(t, x.authReq(t, http.MethodPost, target, body, withToken(agentTok)), "owner's decision")
	assert.Empty(t, x.ctl.decided, "the controller must not decide for the agent")

	assertNotDenied(t, x.authReq(t, http.MethodPost, target, body, withToken(testToken)), "owner token")
	assertNotDenied(t, x.authReq(t, http.MethodPost, target, body, withCookie(cookie)), "owner session")
}

func TestAgentTokenCannotResumeFactory(t *testing.T) {
	x := newHarness(t)
	cookie := x.newUserSession(t, "boss", domain.RoleOwner)

	assertPersonOnly(t, x.authReq(t, http.MethodPost, "/api/v1/control/resume", "", withToken(agentTok)), "owner's decision")
	assert.NotContains(t, x.ctl.calls, "SetPaused:false", "the controller must not resume for the agent")

	assertNotDenied(t, x.authReq(t, http.MethodPost, "/api/v1/control/resume", "", withToken(testToken)), "owner token")
	assertNotDenied(t, x.authReq(t, http.MethodPost, "/api/v1/control/resume", "", withCookie(cookie)), "owner session")

	assertNotDenied(t, x.authReq(t, http.MethodPost, "/api/v1/control/pause", "", withToken(agentTok)), "agent pause")
}

func TestAgentTokenCannotResumeProject(t *testing.T) {
	x := newHarness(t)
	target := "/api/v1/projects/onett/resume"
	cookie := x.newUserSession(t, "boss", domain.RoleOwner)

	assertPersonOnly(t, x.authReq(t, http.MethodPost, target, "", withToken(agentTok)), "owner's decision")
	assert.NotContains(t, x.ctl.calls, "SetProjectPaused:onett:false", "the controller must not resume for the agent")

	assertNotDenied(t, x.authReq(t, http.MethodPost, target, "", withToken(testToken)), "owner token")
	assertNotDenied(t, x.authReq(t, http.MethodPost, target, "", withCookie(cookie)), "owner session")

	assertNotDenied(t, x.authReq(t, http.MethodPost, "/api/v1/projects/onett/pause", "", withToken(agentTok)), "agent pause")
}

func TestAgentTokenCannotRunLoop(t *testing.T) {
	x := newHarness(t)
	target := "/api/v1/loops/heartbeat/run"
	cookie := x.newUserSession(t, "boss", domain.RoleOwner)

	assertPersonOnly(t, x.authReq(t, http.MethodPost, target, "", withToken(agentTok)), "owner's decision")
	assert.Empty(t, x.ctl.loopRun, "the controller must not run a loop for the agent")

	assertNotDenied(t, x.authReq(t, http.MethodPost, target, "", withToken(testToken)), "owner token")
	assertNotDenied(t, x.authReq(t, http.MethodPost, target, "", withCookie(cookie)), "owner session")
}

func TestAgentTokenCannotWriteConversation(t *testing.T) {
	x := newHarness(t)
	cookie := x.newUserSession(t, "boss", domain.RoleOwner)
	send := "/api/v1/conversations/some-id/messages"
	flush := "/api/v1/conversations/some-id/flush"

	assertPersonOnly(t, x.authReq(t, http.MethodPost, send, `{"text":"hi"}`, withToken(agentTok)), "only the owner")
	assertPersonOnly(t, x.authReq(t, http.MethodPost, flush, "", withToken(agentTok)), "only the owner")
	assert.Empty(t, x.ctl.chatText, "the controller must not receive the agent's message")

	for _, tok := range []reqOpt{withToken(testToken), withCookie(cookie)} {
		assertNotDenied(t, x.authReq(t, http.MethodPost, send, `{"text":"hi"}`, tok), "owner")
		assertNotDenied(t, x.authReq(t, http.MethodPost, flush, "", tok), "owner")
	}
}

func TestAgentTokenKeepsReadAndOperate(t *testing.T) {
	x := newHarness(t)
	rec := x.authReq(t, http.MethodGet, "/api/v1/control", "", withToken(agentTok))
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestAgentTokenCannotAdminister(t *testing.T) {
	x := newHarness(t)
	rec := x.authReq(t, http.MethodGet, "/api/v1/users", "", withToken(agentTok))
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

func TestAgentTokenStaysRefusedWhereTheTokenIs(t *testing.T) {
	x := newHarness(t)
	rec := x.authReq(t, http.MethodPost, "/api/v1/tasks/some-id/conversation/reopen", "", withToken(agentTok))
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

func TestPersonOnlyErrorIsAnswered403(t *testing.T) {
	x := newHarness(t)
	s := newProbeServer(x)

	rec := httptest.NewRecorder()
	s.failPerson(rec, fmt.Errorf("%w: the reason", auth.ErrPersonOnly))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "the reason")

	rec = httptest.NewRecorder()
	s.failPerson(rec, store.ErrNotFound)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSetupNeedsTheSetupCode(t *testing.T) {
	x := newHarness(t)
	setup := "/api/v1/auth/setup"
	pw := `"password":"a-long-password-123"`

	rec := x.authReq(t, http.MethodPost, setup, `{"username":"owner",`+pw+`}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, "no code")

	rec = x.authReq(t, http.MethodPost, setup, `{"username":"owner",`+pw+`,"code":"wrong"}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, "wrong code")

	rec = x.authReq(t, http.MethodGet, "/api/v1/auth/state", "")
	assert.JSONEq(t, `{"setup_required":true,"user":null}`, rec.Body.String())

	rec = x.authReq(t, http.MethodPost, setup, fmt.Sprintf(`{"username":"owner",%s,"code":%q}`, pw, auth.SetupCode(testToken)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	sessionCookieFrom(t, rec)
}

func TestSetupRefusedWhenServerHasNoToken(t *testing.T) {
	x := newHarness(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewServer(Options{
		Store: x.st, Bus: x.bus, Controller: x.ctl, Token: "", Version: "test-version",
		Web: fstest.MapFS{}, Logger: logger,
	})
	req := testRequest(http.MethodPost, "/api/v1/auth/setup",
		strings.NewReader(`{"username":"owner","password":"a-long-password-123","code":""}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

func TestLoginStillRejectsUnknownFields(t *testing.T) {
	x := newHarness(t)
	rec := x.authReq(t, http.MethodPost, "/api/v1/auth/login", `{"username":"x","password":"y","code":"z"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

// TestStateDependentRefusalsAreAnswered403 checks that a person-only refusal found by the
// controller (a denial that stopped the task, a goal the owner paused) reaches the agent token
// as 403 with its reason, not as a server error.
func TestStateDependentRefusalsAreAnswered403(t *testing.T) {
	x := newHarness(t)
	x.ctl.errs["RetryTask:t1:"] = fmt.Errorf("%w: the reason", auth.ErrPersonOnly)
	x.ctl.errs["SendTaskMessage"] = fmt.Errorf("%w: the reason", auth.ErrPersonOnly)
	x.ctl.errs["SetGoalStatus"] = fmt.Errorf("%w: the reason", auth.ErrPersonOnly)
	for _, tc := range []struct{ path, body string }{
		{"/api/v1/tasks/t1/retry", ""},
		{"/api/v1/tasks/t1/messages", `{"text":"go on"}`},
		{"/api/v1/goals/g1/resume", ""},
	} {
		rec := x.authReq(t, http.MethodPost, tc.path, tc.body, withToken(agentTok))
		assertPersonOnly(t, rec, "the reason")
	}
}
