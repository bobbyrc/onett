package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

var triggerBase = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func TestTriggerRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	next := triggerBase.Add(time.Hour)

	tr, err := s.CreateTrigger(ctx, domain.Trigger{
		Name: "nightly", Kind: domain.TriggerSchedule, Project: "p", Prompt: "do it",
		Enabled: true, Cron: "0 5 * * *", TimeZone: "America/Chicago", NextRunAt: &next,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, tr.ID)
	assert.False(t, tr.CreatedAt.IsZero())
	assert.False(t, tr.UpdatedAt.IsZero())

	got, err := s.GetTrigger(ctx, tr.ID)
	require.NoError(t, err)
	assert.Equal(t, "nightly", got.Name)
	assert.Equal(t, domain.TriggerSchedule, got.Kind)
	assert.Equal(t, "p", got.Project)
	assert.Equal(t, "do it", got.Prompt)
	assert.True(t, got.Enabled)
	assert.Equal(t, "0 5 * * *", got.Cron)
	assert.Equal(t, "America/Chicago", got.TimeZone)
	require.NotNil(t, got.NextRunAt)
	assert.True(t, next.Equal(*got.NextRunAt))

	_, err = s.GetTrigger(ctx, "nope")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestListTriggersOldestFirst(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	empty, err := s.ListTriggers(ctx)
	require.NoError(t, err)
	assert.Len(t, empty, 0)

	for _, c := range []struct {
		name string
		at   time.Time
	}{
		{"middle", triggerBase.Add(2 * time.Hour)},
		{"newest", triggerBase.Add(3 * time.Hour)},
		{"oldest", triggerBase},
	} {
		_, err := s.CreateTrigger(ctx, domain.Trigger{Name: c.name, Kind: domain.TriggerGitHubIssue, Project: "p", CreatedAt: c.at})
		require.NoError(t, err)
	}

	got, err := s.ListTriggers(ctx)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "oldest", got[0].Name)
	assert.Equal(t, "middle", got[1].Name)
	assert.Equal(t, "newest", got[2].Name)
}

func TestUpdateTriggerSavesAndStamps(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	tr, err := s.CreateTrigger(ctx, domain.Trigger{Name: "a", Kind: domain.TriggerSchedule, Project: "p", Enabled: true})
	require.NoError(t, err)

	up, err := s.UpdateTrigger(ctx, tr.ID, func(x *domain.Trigger) error {
		x.Enabled, x.ConversationID = false, "c1"
		return nil
	})
	require.NoError(t, err)
	assert.False(t, up.Enabled)
	assert.Equal(t, "c1", up.ConversationID)
	assert.False(t, up.UpdatedAt.Before(tr.UpdatedAt))

	got, err := s.GetTrigger(ctx, tr.ID)
	require.NoError(t, err)
	assert.False(t, got.Enabled)
	assert.Equal(t, "c1", got.ConversationID)

	boom := assert.AnError
	_, err = s.UpdateTrigger(ctx, tr.ID, func(x *domain.Trigger) error {
		x.Name = "changed"
		return boom
	})
	assert.ErrorIs(t, err, boom)
	got, err = s.GetTrigger(ctx, tr.ID)
	require.NoError(t, err)
	assert.Equal(t, "a", got.Name)

	_, err = s.UpdateTrigger(ctx, "nope", func(*domain.Trigger) error { return nil })
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestDeleteTriggerRemovesItsFirings(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a, err := s.CreateTrigger(ctx, domain.Trigger{Name: "a", Kind: domain.TriggerSchedule, Project: "p"})
	require.NoError(t, err)
	b, err := s.CreateTrigger(ctx, domain.Trigger{Name: "b", Kind: domain.TriggerSchedule, Project: "p"})
	require.NoError(t, err)
	for _, id := range []string{a.ID, a.ID} {
		_, err := s.CreateTriggerFiring(ctx, domain.TriggerFiring{TriggerID: id, Outcome: domain.FiringStarted})
		require.NoError(t, err)
	}
	_, err = s.CreateTriggerFiring(ctx, domain.TriggerFiring{TriggerID: b.ID, Outcome: domain.FiringStarted})
	require.NoError(t, err)

	require.NoError(t, s.DeleteTrigger(ctx, a.ID))

	_, err = s.GetTrigger(ctx, a.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
	fa, err := s.ListTriggerFirings(ctx, a.ID, 0)
	require.NoError(t, err)
	assert.Empty(t, fa)
	fb, err := s.ListTriggerFirings(ctx, b.ID, 0)
	require.NoError(t, err)
	assert.Len(t, fb, 1)
	_, err = s.GetTrigger(ctx, b.ID)
	require.NoError(t, err)

	assert.ErrorIs(t, s.DeleteTrigger(ctx, a.ID), store.ErrNotFound)
}

func TestTriggerFiringsNewestFirstAndLimited(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	tr, err := s.CreateTrigger(ctx, domain.Trigger{Name: "a", Kind: domain.TriggerSchedule, Project: "p"})
	require.NoError(t, err)
	t1, t2, t3 := triggerBase, triggerBase.Add(time.Hour), triggerBase.Add(2*time.Hour)
	for _, at := range []time.Time{t2, t3, t1} {
		_, err := s.CreateTriggerFiring(ctx, domain.TriggerFiring{TriggerID: tr.ID, At: at, Outcome: domain.FiringStarted})
		require.NoError(t, err)
	}

	all, err := s.ListTriggerFirings(ctx, tr.ID, 0)
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.True(t, t3.Equal(all[0].At))
	assert.True(t, t2.Equal(all[1].At))
	assert.True(t, t1.Equal(all[2].At))

	two, err := s.ListTriggerFirings(ctx, tr.ID, 2)
	require.NoError(t, err)
	require.Len(t, two, 2)
	assert.True(t, t3.Equal(two[0].At))
	assert.True(t, t2.Equal(two[1].At))

	f, err := s.CreateTriggerFiring(ctx, domain.TriggerFiring{TriggerID: tr.ID, Outcome: domain.FiringSkipped})
	require.NoError(t, err)
	assert.NotEmpty(t, f.ID)
	assert.False(t, f.At.IsZero())
}

func TestUpdateTriggerFiring(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	f, err := s.CreateTriggerFiring(ctx, domain.TriggerFiring{TriggerID: "t", Outcome: domain.FiringStarted})
	require.NoError(t, err)

	up, err := s.UpdateTriggerFiring(ctx, f.ID, func(x *domain.TriggerFiring) error {
		x.Outcome, x.Reason, x.MessageID = domain.FiringFailed, "x", "m1"
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, domain.FiringFailed, up.Outcome)

	list, err := s.ListTriggerFirings(ctx, "t", 0)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, domain.FiringFailed, list[0].Outcome)
	assert.Equal(t, "x", list[0].Reason)
	assert.Equal(t, "m1", list[0].MessageID)

	_, err = s.UpdateTriggerFiring(ctx, "nope", func(*domain.TriggerFiring) error { return nil })
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestLastIssueFiringIsTheNewestForThatIssue(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	t1, t2, t3, t4 := triggerBase, triggerBase.Add(time.Hour), triggerBase.Add(2*time.Hour), triggerBase.Add(3*time.Hour)
	for _, f := range []domain.TriggerFiring{
		{TriggerID: "t", IssueNumber: 7, At: t1, Outcome: domain.FiringSkipped},
		{TriggerID: "t", IssueNumber: 7, At: t2, Outcome: domain.FiringStarted},
		{TriggerID: "t", IssueNumber: 8, At: t3, Outcome: domain.FiringSkipped},
		{TriggerID: "t", IssueNumber: 0, At: t4, Outcome: domain.FiringStarted},
	} {
		_, err := s.CreateTriggerFiring(ctx, f)
		require.NoError(t, err)
	}

	got, err := s.LastIssueFiring(ctx, "t", 7)
	require.NoError(t, err)
	assert.True(t, t2.Equal(got.At))
	assert.Equal(t, domain.FiringStarted, got.Outcome)

	got, err = s.LastIssueFiring(ctx, "t", 8)
	require.NoError(t, err)
	assert.True(t, t3.Equal(got.At))

	_, err = s.LastIssueFiring(ctx, "t", 9)
	assert.ErrorIs(t, err, store.ErrNotFound)

	_, err = s.LastIssueFiring(ctx, "other", 7)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestTriggersAndFiringsSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onett.db")
	ctx := context.Background()

	s, err := Open(path)
	require.NoError(t, err)
	tr, err := s.CreateTrigger(ctx, domain.Trigger{Name: "a", Kind: domain.TriggerGitHubIssue, Project: "p"})
	require.NoError(t, err)
	_, err = s.CreateTriggerFiring(ctx, domain.TriggerFiring{TriggerID: tr.ID, IssueNumber: 5, Outcome: domain.FiringStarted})
	require.NoError(t, err)
	require.NoError(t, s.Close())

	s, err = Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	got, err := s.GetTrigger(ctx, tr.ID)
	require.NoError(t, err)
	assert.Equal(t, "a", got.Name)
	f, err := s.LastIssueFiring(ctx, tr.ID, 5)
	require.NoError(t, err)
	assert.Equal(t, domain.FiringStarted, f.Outcome)
}
