package engine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/scm"
)

// trvHookSCM runs before a ListOpenedIssues call returns, so a test can change the trigger
// while the tick is listing issues.
type trvHookSCM struct {
	scm.SCM
	before func()
}

func (h *trvHookSCM) ListOpenedIssues(ctx context.Context, since time.Time) ([]scm.Issue, error) {
	h.before()
	return h.SCM.ListOpenedIssues(ctx, since)
}

// trvOnFirstListing makes the demo project's SCM run fn once, inside the first issue listing.
func trvOnFirstListing(r *ctRig, e *Engine, fn func()) {
	var once sync.Once
	e.d.SCMFor = func(*config.Project) (scm.SCM, error) {
		return &trvHookSCM{SCM: r.fk, before: func() { once.Do(fn) }}, nil
	}
}

func TestIssueTriggerDisabledDuringTheTickDoesNotFire(t *testing.T) {
	ctx := context.Background()
	r, e, now, calls := trtSetup(t)
	tr := trtIssueTrigger(t, e, "Triage", "Read it.", "")
	t0 := *tr.IssuesAfter
	trtIssue(r, t0.Add(time.Minute), "OWNER", "Crash")

	off := false
	trvOnFirstListing(r, e, func() {
		_, err := e.UpdateTrigger(ctx, tr.ID, domain.TriggerPatch{Enabled: &off}, "owner:alice")
		require.NoError(t, err)
	})
	trtTick(t, e, now, t0.Add(2*time.Minute))
	trtSettle(t, r, e)

	assert.Empty(t, trtFirings(t, r, tr.ID))
	assert.Empty(t, calls.got())
}

func TestIssueTriggerReenabledDuringTheTickSkipsOlderIssues(t *testing.T) {
	ctx := context.Background()
	r, e, now, calls := trtSetup(t)
	tr := trtIssueTrigger(t, e, "Triage", "Read it.", "")
	t0 := *tr.IssuesAfter
	trtIssue(r, t0.Add(time.Minute), "OWNER", "Crash")

	off, on := false, true
	trvOnFirstListing(r, e, func() {
		*now = now.Add(time.Hour)
		_, err := e.UpdateTrigger(ctx, tr.ID, domain.TriggerPatch{Enabled: &off}, "owner:alice")
		require.NoError(t, err)
		_, err = e.UpdateTrigger(ctx, tr.ID, domain.TriggerPatch{Enabled: &on}, "owner:alice")
		require.NoError(t, err)
	})
	trtTick(t, e, now, t0.Add(2*time.Minute))
	trtSettle(t, r, e)

	assert.Empty(t, trtFirings(t, r, tr.ID), "the issue predates the re-enable")
	assert.Empty(t, calls.got())
}

func TestIssueTriggerDeletedDuringTheTickLeavesNoFiring(t *testing.T) {
	ctx := context.Background()
	r, e, now, calls := trtSetup(t)
	tr := trtIssueTrigger(t, e, "Triage", "Read it.", "")
	t0 := *tr.IssuesAfter
	trtIssue(r, t0.Add(time.Minute), "OWNER", "Crash")

	trvOnFirstListing(r, e, func() {
		require.NoError(t, e.DeleteTrigger(ctx, tr.ID, "owner:alice"))
	})
	trtTick(t, e, now, t0.Add(2*time.Minute))
	trtSettle(t, r, e)

	fs, err := r.st.ListTriggerFirings(ctx, tr.ID, 0)
	require.NoError(t, err)
	assert.Empty(t, fs)
	assert.Empty(t, calls.got())
}

func TestIssueLabelsMatchWithoutRegardToCase(t *testing.T) {
	r, e, now, calls := trtSetup(t)
	intake := strings.ToUpper(e.Config().LoopsFor("demo").IssueIntake.Label)
	friction := strings.ToUpper(e.Config().LoopsFor("demo").Friction.Label)
	require.NotEmpty(t, intake)
	require.NotEmpty(t, friction)

	tr := trtIssueTrigger(t, e, "Bugs", "Read it.", "bug")
	t0 := *tr.IssuesAfter

	fired := trtIssue(r, t0.Add(time.Minute), "OWNER", "Capitalised", "Bug")
	trtTick(t, e, now, t0.Add(2*time.Minute))
	trtSettle(t, r, e)
	fs := trtFirings(t, r, tr.ID)
	require.Len(t, fs, 1)
	assert.Equal(t, fired, fs[0].IssueNumber)

	trtIssue(r, t0.Add(3*time.Minute), "OWNER", "Intake wins", "bug", intake)
	trtIssue(r, t0.Add(4*time.Minute), "OWNER", "Friction", "bug", friction)
	trtTick(t, e, now, t0.Add(5*time.Minute))
	trtSettle(t, r, e)
	assert.Len(t, trtFirings(t, r, tr.ID), 1, "upper-case intake and friction labels still exclude")
	assert.Len(t, calls.got(), 1)
}
