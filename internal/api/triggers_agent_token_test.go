package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/auth"
)

// The agent token (the orchestrator's) may read triggers but never change them, and it may not
// run the trigger loop by hand. These tests keep that true.
func TestAgentTokenCannotChangeTriggers(t *testing.T) {
	x, tc := newTriggerHarness(t)
	tr := seedTrigger(t, x.st, "Morning", time.Now())
	agent := withToken(auth.AgentToken(testToken))

	for _, r := range trigMutations(tr.ID) {
		rec := x.authReq(t, r.method, r.path, r.body, agent)
		assert.Equal(t, http.StatusForbidden, rec.Code, "agent token %s %s: %s", r.method, r.path, rec.Body.String())
	}
	assert.Empty(t, tc.by, "the controller must not be called for a refused request")

	for _, path := range []string{"/api/v1/triggers", "/api/v1/triggers/" + tr.ID, "/api/v1/triggers/" + tr.ID + "/firings"} {
		rec := x.authReq(t, http.MethodGet, path, "", agent)
		assert.Equal(t, http.StatusOK, rec.Code, "agent token GET %s: %s", path, rec.Body.String())
	}

	all, err := x.st.ListTriggers(context.Background())
	require.NoError(t, err)
	require.Len(t, all, 1)
	got, err := x.st.GetTrigger(context.Background(), tr.ID)
	require.NoError(t, err)
	assert.Equal(t, "Morning", got.Name)
	assert.True(t, got.Enabled)
}

func TestAgentTokenCannotRunTheTriggersLoopByHand(t *testing.T) {
	x := newHarness(t)
	target := "/api/v1/loops/triggers/run"

	assertPersonOnly(t, x.authReq(t, http.MethodPost, target, "", withToken(agentTok)), "owner's decision")
	assert.Empty(t, x.ctl.loopRun, "the controller must not run a loop for the agent")

	assertNotDenied(t, x.authReq(t, http.MethodPost, target, "", withToken(testToken)), "owner token")
}
