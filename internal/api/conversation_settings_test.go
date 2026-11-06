package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	agentharness "github.com/bobbyrc/onett/internal/harness"
)

func (f *fakeCtl) ConversationSettings(_ context.Context, id string) (SettingsView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.note("ConversationSettings"); err != nil {
		return SettingsView{}, err
	}
	return SettingsView{
		Settings: f.settings, Harness: "claude",
		Choices: agentharness.Choices{Efforts: []agentharness.Choice{{Value: "high", Label: "High"}}},
	}, nil
}

func (f *fakeCtl) UpdateConversationSettings(_ context.Context, id string, in domain.ConversationSettings, confirm bool) (SettingsView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.note("UpdateConversationSettings"); err != nil {
		return SettingsView{}, err
	}
	f.settings, f.confirmed = in, confirm
	return SettingsView{Settings: in, Harness: "claude"}, nil
}

func TestConversationSettingsRoutes(t *testing.T) {
	x := newHarness(t)
	person := x.newUserSession(t, "alice", domain.RoleOperator)

	rec := x.authReq(t, http.MethodPut, "/api/v1/conversations/c1/settings",
		`{"model":"opus","effort":"high","permission_mode":"plan","confirm_dangerous":true}`, withCookie(person))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, domain.ConversationSettings{Model: "opus", Effort: "high", PermissionMode: "plan"}, x.ctl.settings)
	assert.True(t, x.ctl.confirmed)

	rec = x.authReq(t, http.MethodGet, "/api/v1/conversations/c1/settings", "", withCookie(person))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var v SettingsView
	decode(t, rec, &v)
	assert.Equal(t, "opus", v.Settings.Model)
	assert.Equal(t, "high", v.Choices.Efforts[0].Value)

	rec = x.authReq(t, http.MethodPut, "/api/v1/conversations/c1/settings", `{"bogus":1}`, withCookie(person))
	assert.Equal(t, http.StatusBadRequest, rec.Code, "unknown fields are errors")

	x.ctl.errs["UpdateConversationSettings"] = ErrInvalid
	rec = x.authReq(t, http.MethodPut, "/api/v1/conversations/c1/settings", `{}`, withCookie(person))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// The permission mode decides what the agent may do unasked, so the agent's own API token cannot
// change it (it can still read it).
func TestConversationSettingsRefuseToken(t *testing.T) {
	x := newHarness(t)
	rec := x.call(t, http.MethodPut, "/api/v1/conversations/c1/settings", `{"permission_mode":"plan"}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	rec = x.call(t, http.MethodGet, "/api/v1/conversations/c1/settings", "")
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}
