package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/notify"
)

const hbAuthError = "Failed to authenticate: this login reached its time limit and cannot be renewed. Run claude auth login"

// harnessRun stores a chat run of the orchestrator's harness that ended at the harness's login wall.
func (h *hbMemEnv) harnessRun(status domain.RunStatus, errText string, at time.Time) domain.Run {
	h.t.Helper()
	r, err := h.st.CreateRun(h.ctx, domain.Run{Project: "demo", Role: chatAgentName, Harness: "claude",
		Status: status, Error: errText, StartedAt: at, EndedAt: &at})
	require.NoError(h.t, err)
	return r
}

func (h *hbMemEnv) authItems() []HeartbeatItem {
	var out []HeartbeatItem
	for _, d := range h.rec.digests {
		for _, it := range d.Items {
			if it.Kind == hbHarnessAuth {
				out = append(out, it)
			}
		}
	}
	return out
}

func TestHeartbeatCollapsesAuthFailuresIntoOneItem(t *testing.T) {
	h := hbMemNew(t, "")
	var runs []domain.Run
	for i := 0; i < 5; i++ {
		runs = append(runs, h.harnessRun(domain.RunFailed, hbAuthError, h.now.Add(-time.Duration(30-i)*time.Minute)))
	}
	h.tick()

	require.Len(t, h.rec.digests, 0, "the orchestrator's own harness is down: no wake")
	mem := h.memory()
	require.Len(t, mem.Items, 1)
	var it HeartbeatItem
	items, err := h.e.heartbeatItems(h.ctx, h.e.Config().Projects["demo"], time.Hour, h.now.Add(-time.Hour))
	require.NoError(t, err)
	require.Len(t, items, 1, "one item, not one per run")
	it = items[0]
	assert.Equal(t, hbHarnessAuth, it.Kind)
	assert.Equal(t, "claude", it.EntityID)
	assert.Contains(t, it.Summary, "5 run(s)")
	for _, r := range runs {
		assert.NotContains(t, it.Key, r.ID)
	}
}

func TestHeartbeatAuthItemEscalatesOnceAndWakesNothing(t *testing.T) {
	h := hbMemNew(t, "")
	h.harnessRun(domain.RunFailed, hbAuthError, h.now.Add(-10*time.Minute))
	blocked := h.blocked("Waits for a wake")
	h.tick()
	for i := 0; i < 6; i++ {
		// Each tick the failed turn would have added a run; none is started, but an old one stays.
		h.tick()
	}
	assert.Empty(t, h.rec.digests, "no wake while the login item is open")
	var auth []notify.Notification
	for _, n := range h.notes {
		if n.Priority == notify.PriorityHigh && strings.Contains(n.Title, hbHarnessAuth) {
			auth = append(auth, n)
		}
	}
	require.Len(t, auth, 1, "one escalation, not one per tick: %+v", h.notes)
	assert.Contains(t, auth[0].Title, "claude")

	// A run that works clears the item, and the held-back items then wake the orchestrator.
	h.harnessRun(domain.RunSucceeded, "", h.now.Add(time.Minute))
	h.tick()
	require.Len(t, h.rec.digests, 1)
	d := h.rec.digests[0]
	assert.True(t, hbHas(d, hbBlockedTask, blocked.ID))
	assert.Empty(t, h.authItems())
}
