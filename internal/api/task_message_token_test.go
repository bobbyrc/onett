package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/auth"
	"github.com/bobbyrc/onett/internal/domain"
)

func TestAgentTokenTaskMessageIsTheOrchestrators(t *testing.T) {
	x := newHarness(t)

	rec := x.authReq(t, http.MethodPost, "/api/v1/tasks/t1/messages", `{"text":"unblock: use tabs"}`, withToken(auth.AgentToken(testToken)))
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var m domain.ChatMessage
	decode(t, rec, &m)
	assert.Equal(t, domain.AuthorOrchestrator, m.Author)
	assert.Equal(t, domain.AuthorOrchestrator, x.ctl.taskMsgAuthor)
	assert.Equal(t, "unblock: use tabs", x.ctl.chatText)

	// The owner's own CLI speaks as the owner: no author.
	rec = x.authReq(t, http.MethodPost, "/api/v1/tasks/t1/messages", `{"text":"owner says hi"}`, withToken(testToken))
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var om domain.ChatMessage
	decode(t, rec, &om)
	assert.Empty(t, om.Author)
	assert.Empty(t, x.ctl.taskMsgAuthor)

	// A person's message keeps the owner's voice: no author.
	cookie := x.newUserSession(t, "alice", domain.RoleOperator)
	rec = x.authReq(t, http.MethodPost, "/api/v1/tasks/t1/messages", `{"text":"hi"}`, withCookie(cookie))
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var pm domain.ChatMessage
	decode(t, rec, &pm)
	assert.Empty(t, pm.Author)
	assert.Empty(t, x.ctl.taskMsgAuthor)
}

func TestTokenStaysPersonOnlyForTheOwnersWords(t *testing.T) {
	x := newHarness(t)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPatch, "/api/v1/messages/m1", `{"text":"x"}`},
		{http.MethodDelete, "/api/v1/messages/m1", ""},
		{http.MethodPost, "/api/v1/tasks/t1/conversation/flush", ""},
		{http.MethodPost, "/api/v1/tasks/t1/conversation/reopen", ""},
	} {
		rec := x.authReq(t, tc.method, tc.path, tc.body, withToken(testToken))
		assert.Equal(t, http.StatusForbidden, rec.Code, tc.method+" "+tc.path)
	}
	// Administer actions are still refused to the token.
	rec := x.authReq(t, http.MethodGet, "/api/v1/config/file", "", withToken(testToken))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestTokenCanCancelAndRetryTask(t *testing.T) {
	x := newHarness(t)
	rec := x.authReq(t, http.MethodPost, "/api/v1/tasks/t1/cancel", "", withToken(testToken))
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	rec = x.authReq(t, http.MethodPost, "/api/v1/tasks/t1/retry", "", withToken(testToken))
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}
