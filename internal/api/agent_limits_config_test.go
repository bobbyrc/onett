package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/onett/internal/domain"
)

// setAgentLimits changes the live config the controller returns, as a config reload does.
func setAgentLimits(x *harness, approve, resume bool) {
	x.ctl.mu.Lock()
	defer x.ctl.mu.Unlock()
	x.ctl.cfg.Chat.AgentMayApproveMerges = approve
	x.ctl.cfg.Chat.AgentMayResume = resume
}

func TestAgentMayApproveMergesSetting(t *testing.T) {
	x := newHarness(t)
	target := "/api/v1/approvals/some-id/decide"
	body := `{"approve":true}`
	cookie := x.newUserSession(t, "boss", domain.RoleOwner)

	setAgentLimits(x, false, false)
	assertPersonOnly(t, x.authReq(t, http.MethodPost, target, body, withToken(agentTok)), "owner's decision")
	assertNotDenied(t, x.authReq(t, http.MethodPost, target, body, withToken(testToken)), "owner token")
	assertNotDenied(t, x.authReq(t, http.MethodPost, target, body, withCookie(cookie)), "owner session")

	setAgentLimits(x, true, false)
	assertNotDenied(t, x.authReq(t, http.MethodPost, target, body, withToken(agentTok)), "agent with approve setting on")
	assertNotDenied(t, x.authReq(t, http.MethodPost, target, body, withToken(testToken)), "owner token")
	assertNotDenied(t, x.authReq(t, http.MethodPost, target, body, withCookie(cookie)), "owner session")

	setAgentLimits(x, false, true)
	assertPersonOnly(t, x.authReq(t, http.MethodPost, target, body, withToken(agentTok)), "owner's decision")
}

func TestAgentMayResumeSetting(t *testing.T) {
	x := newHarness(t)
	cookie := x.newUserSession(t, "boss", domain.RoleOwner)
	targets := []string{"/api/v1/control/resume", "/api/v1/projects/onett/resume"}

	setAgentLimits(x, false, false)
	for _, target := range targets {
		assertPersonOnly(t, x.authReq(t, http.MethodPost, target, "", withToken(agentTok)), "owner's decision")
		assertNotDenied(t, x.authReq(t, http.MethodPost, target, "", withToken(testToken)), "owner token")
		assertNotDenied(t, x.authReq(t, http.MethodPost, target, "", withCookie(cookie)), "owner session")
	}

	setAgentLimits(x, false, true)
	for _, target := range targets {
		assertNotDenied(t, x.authReq(t, http.MethodPost, target, "", withToken(agentTok)), "agent with resume setting on")
		assertNotDenied(t, x.authReq(t, http.MethodPost, target, "", withToken(testToken)), "owner token")
		assertNotDenied(t, x.authReq(t, http.MethodPost, target, "", withCookie(cookie)), "owner session")
	}

	setAgentLimits(x, true, false)
	for _, target := range targets {
		assertPersonOnly(t, x.authReq(t, http.MethodPost, target, "", withToken(agentTok)), "owner's decision")
	}
}

func TestOtherRefusalsIgnoreTheSettings(t *testing.T) {
	x := newHarness(t)
	setAgentLimits(x, true, true)
	assertPersonOnly(t, x.authReq(t, http.MethodPost, "/api/v1/loops/heartbeat/run", "", withToken(agentTok)), "owner's decision")
	assertPersonOnly(t, x.authReq(t, http.MethodPost, "/api/v1/conversations/some-id/messages", `{"text":"hi"}`, withToken(agentTok)), "only the owner")
	assertPersonOnly(t, x.authReq(t, http.MethodPost, "/api/v1/conversations/some-id/flush", "", withToken(agentTok)), "only the owner")
}

func TestSettingChangeAppliesWithoutRestart(t *testing.T) {
	x := newHarness(t)
	target := "/api/v1/control/resume"

	setAgentLimits(x, false, false)
	assertPersonOnly(t, x.authReq(t, http.MethodPost, target, "", withToken(agentTok)), "owner's decision")

	setAgentLimits(x, false, true)
	assert.NotEqual(t, http.StatusForbidden, x.authReq(t, http.MethodPost, target, "", withToken(agentTok)).Code, "owner's decision")

	setAgentLimits(x, false, false)
	assertPersonOnly(t, x.authReq(t, http.MethodPost, target, "", withToken(agentTok)), "owner's decision")
}
