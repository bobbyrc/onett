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

// hbNeedsYou returns the escalation notifications; the loop-failure alert is a separate one.
func hbNeedsYou(h *hbMemEnv) []notify.Notification {
	var out []notify.Notification
	for _, n := range h.notes {
		if strings.HasPrefix(n.Title, "Needs you:") {
			out = append(out, n)
		}
	}
	return out
}

// hbFailTick advances the clock one interval and runs the loop, which must report the wake error.
func hbFailTick(h *hbMemEnv) {
	h.t.Helper()
	h.now = h.now.Add(15 * time.Minute)
	require.Error(h.t, h.e.RunLoop(h.ctx, loopHeartbeat, "demo"))
}

func TestHeartbeatFailedWakeStillEscalates(t *testing.T) {
	h := hbMemNew(t, "")
	a := h.blocked("A")
	h.rec.err = assert.AnError // the model call is what is broken

	hbFailTick(h)
	hbFailTick(h)
	assert.Empty(t, hbNeedsYou(h), "two heartbeats so far")
	hbFailTick(h) // the third heartbeat escalates although the orchestrator was never woken
	require.Len(t, hbNeedsYou(h), 1)
	assert.Contains(t, hbNeedsYou(h)[0].Title, a.ID)
	assert.Contains(t, hbNeedsYou(h)[0].Body, "No action tried yet.")
	assert.Empty(t, h.rec.digests)
	require.Len(t, h.memory().Items, 1)
	for _, m := range h.memory().Items {
		assert.Equal(t, 3, m.Heartbeats)
		assert.True(t, m.Escalated)
		assert.True(t, m.Unwoken)
	}

	h.rec.err = nil // the model is back: the item is still new to the orchestrator
	h.tick()
	require.Len(t, h.rec.digests, 1)
	assert.Len(t, h.rec.digests[0].NewKeys, 1)
	for _, m := range h.memory().Items {
		assert.False(t, m.Unwoken)
		assert.Equal(t, 4, m.Heartbeats)
	}
	h.tick()
	assert.Len(t, h.rec.digests, 1, "shown once, so not new any more")
	assert.Len(t, hbNeedsYou(h), 1, "escalated once")
}

func TestHeartbeatFailedWakeKeepsTheWindowOpen(t *testing.T) {
	h := hbMemNew(t, "")
	task := h.blocked("A")
	ended := h.now.Add(-30 * time.Minute)
	run, err := h.st.CreateRun(h.ctx, domain.Run{TaskID: task.ID, Project: "demo", Role: "implementer", Status: domain.RunStalled, StartedAt: ended, EndedAt: &ended})
	require.NoError(t, err)

	h.rec.err = assert.AnError
	hbFailTick(h)
	hbFailTick(h)
	h.rec.err = nil
	h.tick()
	require.Len(t, h.rec.digests, 1)
	assert.True(t, hbHas(h.rec.digests[0], hbRun, run.ID), "the run is still in the first digest the orchestrator sees")
}
