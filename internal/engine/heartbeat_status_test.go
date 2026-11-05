package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (h *hbMemEnv) heartbeatRow() (row struct {
	Actions, Escalations int
	Ops                  string
}) {
	h.t.Helper()
	for _, r := range h.e.Loops(h.ctx) {
		if r.Name == loopHeartbeat && r.Project == "demo" {
			row.Actions, row.Escalations, row.Ops = r.Actions24h, r.Escalations24h, r.OperationsConversationID
			return row
		}
	}
	h.t.Fatal("no heartbeat/demo row")
	return row
}

func TestHeartbeatStatusCountsAndPrunesRollingLog(t *testing.T) {
	h := hbMemNew(t, "")
	a := h.blocked("A")
	h.tick()
	h.reply(a.ID + ": retried it\n" + a.ID + ": escalate: still stuck")
	h.tick()
	h.tick() // third heartbeat: escalates once

	row := h.heartbeatRow()
	assert.Equal(t, 2, row.Actions)
	assert.Equal(t, 1, row.Escalations)
	assert.Equal(t, h.conv.ID, row.Ops)

	// A day later the entries are out of the window: the view stops counting them at once, and
	// the next tick prunes them from the stored log.
	h.now = h.now.Add(25 * time.Hour)
	row = h.heartbeatRow()
	assert.Zero(t, row.Actions)
	assert.Zero(t, row.Escalations)
	h.run(loopHeartbeat)
	mem := h.memory()
	assert.Empty(t, mem.ActionTimes)
	assert.Empty(t, mem.EscalationTimes)
}

func TestLoopsViewOmitsHeartbeatFieldsOnOtherLoops(t *testing.T) {
	h := hbMemNew(t, "")
	for _, r := range h.e.Loops(h.ctx) {
		if r.Name != loopHeartbeat {
			require.Empty(t, r.OperationsConversationID, r.Name)
			require.Zero(t, r.Actions24h, r.Name)
		}
	}
}
