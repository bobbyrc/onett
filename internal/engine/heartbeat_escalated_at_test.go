package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEscalationRecordsWhen(t *testing.T) {
	h := hbMemNew(t, "")
	a := h.blocked("A")
	h.tick()
	h.tick()
	h.reply(a.ID + ": retried it")
	h.tick() // third heartbeat: the item escalates
	at := h.now

	items := h.memory().Items
	require.NotEmpty(t, items)
	for key, m := range items {
		assert.True(t, m.Escalated, key)
		assert.True(t, m.EscalatedAt.Equal(at), "%s escalated at %v, want %v", key, m.EscalatedAt, at)
	}
}
