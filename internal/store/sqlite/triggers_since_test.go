package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestTriggerFiringsSince(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := triggerBase

	mk := func(trigger string, at time.Time) {
		_, err := s.CreateTriggerFiring(ctx, domain.TriggerFiring{
			TriggerID: trigger, At: at, Cause: domain.FiringCauseManual, Outcome: domain.FiringStarted,
		})
		require.NoError(t, err)
	}
	mk("A", now.Add(-90*time.Minute))
	mk("A", now.Add(-59*time.Minute))
	mk("A", now.Add(-10*time.Minute))
	mk("A", now.Add(-60*time.Minute))
	mk("B", now.Add(-5*time.Minute))

	got, err := s.TriggerFiringsSince(ctx, "A", now.Add(-60*time.Minute))
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.True(t, got[0].At.Equal(now.Add(-10*time.Minute)))
	assert.True(t, got[1].At.Equal(now.Add(-59*time.Minute)))
	assert.True(t, got[2].At.Equal(now.Add(-60*time.Minute)), "a firing exactly at since is included")
	for _, f := range got {
		assert.Equal(t, "A", f.TriggerID)
	}

	none, err := s.TriggerFiringsSince(ctx, "unknown", now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Len(t, none, 0)
}
