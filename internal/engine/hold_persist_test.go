package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/harness"
)

func TestHoldSurvivesRestartAndClearIsPersisted(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	reset := e.d.Now().Add(time.Hour)
	e.noteRateLimit(ctx, "claude", harness.RateLimit{ResetAt: reset, Message: "limit"})

	restarted := r.engine(nil)
	require.NoError(t, restarted.recover(ctx))
	h, ok := restarted.holds.held("claude", restarted.d.Now())
	require.True(t, ok)
	assert.Equal(t, 1, h.Hits)
	assert.Equal(t, "limit", h.Message)
	assert.True(t, h.Until.Equal(reset.Add(holdMargin)))

	restarted.clearHold(ctx, "claude")
	again := r.engine(nil)
	require.NoError(t, again.recover(ctx))
	assert.Empty(t, again.Holds(), "a cleared hold stays cleared")
}
