package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestListAllTriggerFiringsIsNewestFirstAcrossTriggers(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, err := s.CreateTrigger(ctx, domain.Trigger{Name: "a", Kind: domain.TriggerSchedule, Project: "p"})
	require.NoError(t, err)
	b, err := s.CreateTrigger(ctx, domain.Trigger{Name: "b", Kind: domain.TriggerSchedule, Project: "p"})
	require.NoError(t, err)

	for _, f := range []domain.TriggerFiring{
		{TriggerID: a.ID, At: triggerBase.Add(1 * time.Minute)},
		{TriggerID: b.ID, At: triggerBase.Add(2 * time.Minute)},
		{TriggerID: a.ID, At: triggerBase.Add(3 * time.Minute)},
	} {
		_, err := s.CreateTriggerFiring(ctx, f)
		require.NoError(t, err)
	}

	got, err := s.ListAllTriggerFirings(ctx, 0)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.True(t, got[0].At.Equal(triggerBase.Add(3*time.Minute)))
	assert.True(t, got[1].At.Equal(triggerBase.Add(2*time.Minute)))
	assert.True(t, got[2].At.Equal(triggerBase.Add(1*time.Minute)))
	assert.Equal(t, []string{a.ID, b.ID, a.ID}, []string{got[0].TriggerID, got[1].TriggerID, got[2].TriggerID})
}

func TestListAllTriggerFiringsHonoursLimitAndDefaultsTo50(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	tr, err := s.CreateTrigger(ctx, domain.Trigger{Name: "a", Kind: domain.TriggerSchedule, Project: "p"})
	require.NoError(t, err)
	for i := 0; i < 55; i++ {
		_, err := s.CreateTriggerFiring(ctx, domain.TriggerFiring{
			TriggerID: tr.ID, At: triggerBase.Add(time.Duration(i) * time.Second),
		})
		require.NoError(t, err)
	}

	two, err := s.ListAllTriggerFirings(ctx, 2)
	require.NoError(t, err)
	require.Len(t, two, 2)
	assert.True(t, two[0].At.Equal(triggerBase.Add(54*time.Second)))
	assert.True(t, two[1].At.Equal(triggerBase.Add(53*time.Second)))

	def, err := s.ListAllTriggerFirings(ctx, 0)
	require.NoError(t, err)
	assert.Len(t, def, 50)
}

func TestListAllTriggerFiringsIsEmptyWithoutFirings(t *testing.T) {
	s := newStore(t)
	got, err := s.ListAllTriggerFirings(context.Background(), 0)
	require.NoError(t, err)
	assert.Len(t, got, 0)
}
