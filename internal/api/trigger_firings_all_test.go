package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/auth"
	"github.com/bobbyrc/onett/internal/domain"
)

func TestAllTriggerFiringsListsEveryTriggerNewestFirst(t *testing.T) {
	x, _ := newTriggerHarness(t)
	viewer := x.newUserSession(t, "vic", domain.RoleViewer)
	base := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	a := seedTrigger(t, x.st, "First", base)
	b := seedTrigger(t, x.st, "Second", base)
	f1 := seedFiring(t, x.st, a.ID, domain.FiringStarted, base.Add(1*time.Minute))
	f2 := seedFiring(t, x.st, b.ID, domain.FiringSkipped, base.Add(2*time.Minute))
	f3 := seedFiring(t, x.st, a.ID, domain.FiringStarted, base.Add(3*time.Minute))

	rec := x.authReq(t, http.MethodGet, "/api/v1/trigger-firings", "", withCookie(viewer))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out []domain.TriggerFiring
	decode(t, rec, &out)
	require.Len(t, out, 3)
	assert.Equal(t, []string{f3.ID, f2.ID, f1.ID}, []string{out[0].ID, out[1].ID, out[2].ID})
	assert.Equal(t, []string{a.ID, b.ID, a.ID}, []string{out[0].TriggerID, out[1].TriggerID, out[2].TriggerID})
}

func TestAllTriggerFiringsDefaultsTo50AndTakesALimit(t *testing.T) {
	x, _ := newTriggerHarness(t)
	viewer := x.newUserSession(t, "vic", domain.RoleViewer)
	base := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	tr := seedTrigger(t, x.st, "Many", base)
	for i := 0; i < 55; i++ {
		seedFiring(t, x.st, tr.ID, domain.FiringStarted, base.Add(time.Duration(i)*time.Second))
	}

	rec := x.authReq(t, http.MethodGet, "/api/v1/trigger-firings", "", withCookie(viewer))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out []domain.TriggerFiring
	decode(t, rec, &out)
	assert.Len(t, out, 50)

	rec = x.authReq(t, http.MethodGet, "/api/v1/trigger-firings?limit=3", "", withCookie(viewer))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	out = nil
	decode(t, rec, &out)
	assert.Len(t, out, 3)
}

func TestAllTriggerFiringsIsAnEmptyArrayWithNone(t *testing.T) {
	x, _ := newTriggerHarness(t)
	viewer := x.newUserSession(t, "vic", domain.RoleViewer)

	rec := x.authReq(t, http.MethodGet, "/api/v1/trigger-firings", "", withCookie(viewer))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "[]", strings.TrimSpace(rec.Body.String()))
}

func TestAllTriggerFiringsRefusesABadLimit(t *testing.T) {
	x, _ := newTriggerHarness(t)
	viewer := x.newUserSession(t, "vic", domain.RoleViewer)

	for _, q := range []string{"?limit=-1", "?limit=x"} {
		rec := x.authReq(t, http.MethodGet, "/api/v1/trigger-firings"+q, "", withCookie(viewer))
		assert.Equal(t, http.StatusBadRequest, rec.Code, "%s: %s", q, rec.Body.String())
	}
}

func TestAllTriggerFiringsIsReadableWithTheAgentToken(t *testing.T) {
	x, _ := newTriggerHarness(t)
	agent := withToken(auth.AgentToken(testToken))

	rec := x.authReq(t, http.MethodGet, "/api/v1/trigger-firings", "", agent)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}
