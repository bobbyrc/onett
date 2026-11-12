package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	"github.com/bobbyrc/onett/internal/scm"
	"github.com/bobbyrc/onett/internal/store"
)

// trtSetup builds a rig whose demo project is a GitHub repository, with the clock at
// 2026-11-12 03:00 UTC and a harness that records prompts.
func trtSetup(t *testing.T) (*ctRig, *Engine, *time.Time, *trigHarnessCalls) {
	t.Helper()
	r := ctNewRig(t)
	r.cfg.Projects["demo"].Repo = "o/r"
	e := r.engine(nil)
	now := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	e.d.Now = func() time.Time { return now }
	calls := &trigHarnessCalls{}
	e.d.HarnessFor = func(string, config.Harness) (harness.Harness, error) { return calls.harness(t), nil }
	return r, e, &now, calls
}

// trtTick sets the clock to at and runs the triggers loop once for demo as a scheduled tick.
func trtTick(t *testing.T, e *Engine, now *time.Time, at time.Time) {
	t.Helper()
	*now = at
	require.NoError(t, e.runLoopOnce(context.Background(), loopTriggers, "demo", false))
}

// trtSettle waits until no conversation is busy and every turn goroutine has ended.
func trtSettle(t *testing.T, r *ctRig, e *Engine) {
	t.Helper()
	ctx := context.Background()
	require.Eventually(t, func() bool {
		convs, err := r.st.ListConversations(ctx, 100)
		if err != nil {
			return false
		}
		for _, c := range convs {
			if c.Busy {
				return false
			}
		}
		return true
	}, 10*time.Second, 5*time.Millisecond)
	e.wg.Wait()
}

// trtFirings returns the firings of one trigger, oldest first.
func trtFirings(t *testing.T, r *ctRig, id string) []domain.TriggerFiring {
	t.Helper()
	fs, err := r.st.ListTriggerFirings(context.Background(), id, 0)
	require.NoError(t, err)
	out := make([]domain.TriggerFiring, 0, len(fs))
	for i := len(fs) - 1; i >= 0; i-- {
		out = append(out, fs[i])
	}
	return out
}

func trtScheduleTrigger(t *testing.T, e *Engine, name, prompt string) domain.Trigger {
	t.Helper()
	tr, err := e.CreateTrigger(context.Background(), domain.Trigger{
		Name: name, Kind: domain.TriggerSchedule, Project: "demo", Prompt: prompt,
		Enabled: true, Cron: "0 5 * * *", TimeZone: "UTC",
	}, "owner:alice")
	require.NoError(t, err)
	return tr
}

func trtIssueTrigger(t *testing.T, e *Engine, name, prompt, label string) domain.Trigger {
	t.Helper()
	tr, err := e.CreateTrigger(context.Background(), domain.Trigger{
		Name: name, Kind: domain.TriggerGitHubIssue, Project: "demo", Prompt: prompt,
		Enabled: true, Label: label,
	}, "owner:alice")
	require.NoError(t, err)
	return tr
}

// trtIssue opens an issue in the fake repo, created at at, with the given author association.
func trtIssue(r *ctRig, at time.Time, assoc, title string, labels ...string) int {
	is := r.fk.AddIssue(title, "body of "+title, labels...)
	stored := r.fk.Issues[is.Number]
	stored.CreatedAt = at
	stored.AuthorAssociation = assoc
	stored.Author = "alice"
	return is.Number
}

// trtCountingSCM counts ListOpenedIssues calls and can fail them.
type trtCountingSCM struct {
	scm.SCM
	mu     sync.Mutex
	listed int
	err    error
}

func (c *trtCountingSCM) ListOpenedIssues(ctx context.Context, since time.Time) ([]scm.Issue, error) {
	c.mu.Lock()
	c.listed++
	err := c.err
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return c.SCM.ListOpenedIssues(ctx, since)
}

func (c *trtCountingSCM) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.listed
}

func TestScheduleTriggerFiresWhenDue(t *testing.T) {
	ctx := context.Background()
	r, e, now, calls := trtSetup(t)
	tr := trtScheduleTrigger(t, e, "Nightly", "Sweep the issues.")

	trtTick(t, e, now, time.Date(2026, 11, 12, 4, 59, 0, 0, time.UTC))
	assert.Empty(t, trtFirings(t, r, tr.ID))
	assert.Empty(t, calls.got())

	trtTick(t, e, now, time.Date(2026, 11, 12, 5, 0, 10, 0, time.UTC))
	fs := trtFirings(t, r, tr.ID)
	require.Len(t, fs, 1)
	f := fs[0]
	assert.Equal(t, domain.FiringCauseSchedule, f.Cause)
	require.NotNil(t, f.ScheduledFor)
	assert.True(t, f.ScheduledFor.Equal(time.Date(2026, 11, 12, 5, 0, 0, 0, time.UTC)))
	assert.Equal(t, domain.FiringStarted, f.Outcome)
	assert.NotEmpty(t, f.ConversationID)
	assert.NotEmpty(t, f.MessageID)
	trtSettle(t, r, e)

	prompts := calls.got()
	require.Len(t, prompts, 1)
	assert.Contains(t, prompts[0], "Sweep the issues.")
	assert.Contains(t, prompts[0], "scheduled for 2026-11-12 05:00 UTC")

	stored, err := r.st.GetTrigger(ctx, tr.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.NextRunAt)
	assert.True(t, stored.NextRunAt.Equal(time.Date(2026, 11, 13, 5, 0, 0, 0, time.UTC)), "got %v", stored.NextRunAt)

	trtTick(t, e, now, time.Date(2026, 11, 12, 5, 0, 45, 0, time.UTC))
	trtSettle(t, r, e)
	assert.Len(t, trtFirings(t, r, tr.ID), 1)
}

func TestScheduleTriggerCatchesUpOnceAfterDowntime(t *testing.T) {
	ctx := context.Background()
	r, e, now, _ := trtSetup(t)
	tr := trtScheduleTrigger(t, e, "Nightly", "Sweep the issues.")

	trtTick(t, e, now, time.Date(2026, 11, 15, 9, 30, 0, 0, time.UTC))
	trtSettle(t, r, e)
	fs := trtFirings(t, r, tr.ID)
	require.Len(t, fs, 1)
	require.NotNil(t, fs[0].ScheduledFor)
	assert.True(t, fs[0].ScheduledFor.Equal(time.Date(2026, 11, 12, 5, 0, 0, 0, time.UTC)))

	stored, err := r.st.GetTrigger(ctx, tr.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.NextRunAt)
	assert.True(t, stored.NextRunAt.Equal(time.Date(2026, 11, 16, 5, 0, 0, 0, time.UTC)), "got %v", stored.NextRunAt)

	trtTick(t, e, now, time.Date(2026, 11, 15, 9, 31, 0, 0, time.UTC))
	trtTick(t, e, now, time.Date(2026, 11, 15, 9, 32, 0, 0, time.UTC))
	trtSettle(t, r, e)
	assert.Len(t, trtFirings(t, r, tr.ID), 1)
}

func TestDisabledAndOtherProjectTriggersDoNotFire(t *testing.T) {
	ctx := context.Background()
	r, e, now, calls := trtSetup(t)
	past := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC)

	off, err := e.CreateTrigger(ctx, domain.Trigger{
		Name: "Off", Kind: domain.TriggerSchedule, Project: "demo", Prompt: "p", Cron: "0 2 * * *", TimeZone: "UTC",
	}, "owner:alice")
	require.NoError(t, err)
	_, err = r.st.UpdateTrigger(ctx, off.ID, func(x *domain.Trigger) error {
		x.NextRunAt = &past
		return nil
	})
	require.NoError(t, err)

	elsewhere, err := r.st.CreateTrigger(ctx, domain.Trigger{
		Name: "Elsewhere", Kind: domain.TriggerSchedule, Project: "elsewhere", Prompt: "p",
		Cron: "0 2 * * *", TimeZone: "UTC", Enabled: true, NextRunAt: &past,
	})
	require.NoError(t, err)

	trtTick(t, e, now, time.Date(2026, 11, 12, 5, 0, 0, 0, time.UTC))
	trtSettle(t, r, e)
	assert.Empty(t, trtFirings(t, r, off.ID))
	assert.Empty(t, trtFirings(t, r, elsewhere.ID))
	assert.Empty(t, calls.got())
}

func TestScheduleChangedMeanwhileDoesNotFireTwice(t *testing.T) {
	ctx := context.Background()
	r, e, now, _ := trtSetup(t)
	tr := trtScheduleTrigger(t, e, "Nightly", "Sweep the issues.")

	trtTick(t, e, now, time.Date(2026, 11, 12, 5, 0, 10, 0, time.UTC))
	trtSettle(t, r, e)
	require.Len(t, trtFirings(t, r, tr.ID), 1)
	stored, err := r.st.GetTrigger(ctx, tr.ID)
	require.NoError(t, err)

	// A copy that still names the 2026-11-12 05:00 run, as a second engine tick might read it.
	stale := stored
	staleDue := time.Date(2026, 11, 12, 5, 0, 0, 0, time.UTC)
	stale.NextRunAt = &staleDue
	*now = time.Date(2026, 11, 12, 5, 0, 20, 0, time.UTC)
	require.NoError(t, e.checkSchedule(ctx, stale, *now))
	trtSettle(t, r, e)

	assert.Len(t, trtFirings(t, r, tr.ID), 1)
	after, err := r.st.GetTrigger(ctx, tr.ID)
	require.NoError(t, err)
	require.NotNil(t, after.NextRunAt)
	assert.True(t, after.NextRunAt.Equal(time.Date(2026, 11, 13, 5, 0, 0, 0, time.UTC)))
}

func TestIssueTriggerFiresOncePerIssueAcrossRestart(t *testing.T) {
	r, e, now, calls := trtSetup(t)
	tr := trtIssueTrigger(t, e, "Triage", "Read it.", "")
	trtIssue(r, tr.IssuesAfter.Add(time.Minute), "OWNER", "Crash")

	trtTick(t, e, now, tr.IssuesAfter.Add(2*time.Minute))
	trtSettle(t, r, e)
	fs := trtFirings(t, r, tr.ID)
	require.Len(t, fs, 1)
	assert.Equal(t, domain.FiringCauseGitHubIssue, fs[0].Cause)
	assert.Equal(t, 1, fs[0].IssueNumber)
	assert.NotEmpty(t, fs[0].IssueURL)
	assert.Equal(t, domain.FiringStarted, fs[0].Outcome)

	trtTick(t, e, now, tr.IssuesAfter.Add(3*time.Minute))
	trtTick(t, e, now, tr.IssuesAfter.Add(4*time.Minute))
	trtSettle(t, r, e)
	assert.Len(t, trtFirings(t, r, tr.ID), 1)

	// A second engine on the same store stands for a restart.
	e2 := r.engine(nil)
	e2.d.Now = func() time.Time { return *now }
	e2.d.HarnessFor = e.d.HarnessFor
	trtTick(t, e2, now, tr.IssuesAfter.Add(5*time.Minute))
	trtSettle(t, r, e2)
	assert.Len(t, trtFirings(t, r, tr.ID), 1)
	assert.Len(t, calls.got(), 1)
}

func TestIssueTriggerIgnoresIssuesThatAlreadyExisted(t *testing.T) {
	r, e, now, calls := trtSetup(t)
	tr := trtIssueTrigger(t, e, "Triage", "Read it.", "")
	t0 := *tr.IssuesAfter
	trtIssue(r, t0.Add(-time.Minute), "OWNER", "Old")

	trtTick(t, e, now, t0.Add(time.Minute))
	trtSettle(t, r, e)
	assert.Empty(t, trtFirings(t, r, tr.ID))

	_, err := e.UpdateTrigger(context.Background(), tr.ID, domain.TriggerPatch{Enabled: boolPtr(false)}, "owner:alice")
	require.NoError(t, err)
	trtIssue(r, t0.Add(5*time.Minute), "OWNER", "Filed while disabled")
	trtTick(t, e, now, t0.Add(10*time.Minute))

	t2 := t0.Add(10 * time.Minute)
	got, err := e.UpdateTrigger(context.Background(), tr.ID, domain.TriggerPatch{Enabled: boolPtr(true)}, "owner:alice")
	require.NoError(t, err)
	require.NotNil(t, got.IssuesAfter)
	assert.True(t, got.IssuesAfter.Equal(t2))

	trtTick(t, e, now, t0.Add(11*time.Minute))
	trtSettle(t, r, e)
	assert.Empty(t, trtFirings(t, r, tr.ID), "issues older than the re-enable do not fire")
	assert.Empty(t, calls.got())

	trtIssue(r, t0.Add(12*time.Minute), "OWNER", "Filed after enable")
	trtTick(t, e, now, t0.Add(13*time.Minute))
	trtSettle(t, r, e)
	fs := trtFirings(t, r, tr.ID)
	require.Len(t, fs, 1)
	assert.Equal(t, 3, fs[0].IssueNumber)
}

func TestIssueTriggerIgnoresUntrustedAuthors(t *testing.T) {
	r, e, now, calls := trtSetup(t)
	tr := trtIssueTrigger(t, e, "Triage", "Read it.", "")
	t0 := *tr.IssuesAfter
	for i, a := range []string{"NONE", "CONTRIBUTOR", "FIRST_TIME_CONTRIBUTOR", ""} {
		trtIssue(r, t0.Add(time.Duration(i+1)*time.Minute), a, "Stranger")
	}
	trtTick(t, e, now, t0.Add(10*time.Minute))
	trtSettle(t, r, e)
	assert.Empty(t, trtFirings(t, r, tr.ID))
	assert.Empty(t, calls.got())
	convs, err := r.st.ListConversations(context.Background(), 100)
	require.NoError(t, err)
	assert.Empty(t, convs)

	trtIssue(r, t0.Add(11*time.Minute), "MEMBER", "Member")
	trtIssue(r, t0.Add(12*time.Minute), "COLLABORATOR", "Collaborator")
	trtTick(t, e, now, t0.Add(13*time.Minute))
	trtSettle(t, r, e)
	fs := trtFirings(t, r, tr.ID)
	require.Len(t, fs, 2)
	assert.Equal(t, 5, fs[0].IssueNumber)
	assert.Equal(t, 6, fs[1].IssueNumber)
	assert.Len(t, calls.got(), 2)
}

func TestIssueEvidenceReachesTheAgentRedactedAndLabelled(t *testing.T) {
	const secret = "s3cr3t-value-0123456789"
	t.Setenv("DEMO_API_TOKEN", secret)
	r, e, now, calls := trtSetup(t)
	tr := trtIssueTrigger(t, e, "Triage", "Read it and decide.", "")
	t0 := *tr.IssuesAfter
	is := r.fk.AddIssue("Crash", "It crashes with token "+secret+" in the log.")
	stored := r.fk.Issues[is.Number]
	stored.CreatedAt = t0.Add(time.Minute)
	stored.AuthorAssociation = "OWNER"
	stored.Author = "alice"

	trtTick(t, e, now, t0.Add(2*time.Minute))
	trtSettle(t, r, e)
	prompts := calls.got()
	require.Len(t, prompts, 1)
	p := prompts[0]

	assert.NotContains(t, p, secret)
	idxEnd := strings.Index(p, "----- end of owner instructions ")
	idxEvidence := strings.Index(p, evidenceNote)
	require.GreaterOrEqual(t, idxEnd, 0)
	require.GreaterOrEqual(t, idxEvidence, 0)
	assert.Greater(t, idxEvidence, idxEnd)
	idxOwner := strings.Index(p, "----- owner instructions ")
	idxPrompt := strings.Index(p, "Read it and decide.")
	assert.True(t, idxOwner < idxPrompt && idxPrompt < idxEnd, "owner text sits between the marker lines")
	idxCrash := strings.Index(p, "It crashes with token")
	assert.Greater(t, idxCrash, idxEvidence)
}

func TestIssueTriggerLabelIntakeAndFrictionFilters(t *testing.T) {
	r, e, now, calls := trtSetup(t)
	intake := e.Config().LoopsFor("demo").IssueIntake.Label
	friction := e.Config().LoopsFor("demo").Friction.Label
	require.True(t, loopOn(e.Config(), loopIssueIntake, "demo"), "issue intake is on for demo")
	require.NotEmpty(t, intake)
	require.NotEmpty(t, friction)

	bug := trtIssueTrigger(t, e, "Bugs", "Read it.", "bug")
	t0 := *bug.IssuesAfter

	// (a) no label: no firing; with the label: one firing.
	n := trtIssue(r, t0.Add(time.Minute), "OWNER", "Unlabelled")
	trtTick(t, e, now, t0.Add(2*time.Minute))
	trtSettle(t, r, e)
	assert.Empty(t, trtFirings(t, r, bug.ID))

	r.fk.Issues[n].Labels = append(r.fk.Issues[n].Labels, "bug")
	trtTick(t, e, now, t0.Add(3*time.Minute))
	trtSettle(t, r, e)
	require.Len(t, trtFirings(t, r, bug.ID), 1)

	// (b) label plus intake label: intake takes it, so no firing.
	trtIssue(r, t0.Add(4*time.Minute), "OWNER", "Intake wins", "bug", intake)
	trtTick(t, e, now, t0.Add(5*time.Minute))
	trtSettle(t, r, e)
	assert.Len(t, trtFirings(t, r, bug.ID), 1)

	// (c) label plus friction label: an issue onett filed itself, so no firing.
	trtIssue(r, t0.Add(6*time.Minute), "OWNER", "Friction", "bug", friction)
	trtTick(t, e, now, t0.Add(7*time.Minute))
	trtSettle(t, r, e)
	assert.Len(t, trtFirings(t, r, bug.ID), 1)

	// (d) a trigger without a label fires for an unlabelled issue.
	plain := trtIssueTrigger(t, e, "Any", "Read it.", "")
	t1 := *plain.IssuesAfter
	trtIssue(r, t1.Add(time.Minute), "OWNER", "Plain")
	trtTick(t, e, now, t1.Add(2*time.Minute))
	trtSettle(t, r, e)
	fs := trtFirings(t, r, plain.ID)
	require.Len(t, fs, 1)
	assert.Equal(t, domain.FiringStarted, fs[0].Outcome)
	assert.Len(t, trtFirings(t, r, bug.ID), 1)
	assert.Len(t, calls.got(), 2)
}

func TestScheduleFiringWhilePausedIsRecordedAsSkipped(t *testing.T) {
	ctx := context.Background()
	r, e, now, calls := trtSetup(t)
	tr := trtScheduleTrigger(t, e, "Nightly", "Sweep the issues.")
	require.NoError(t, e.SetPaused(ctx, true))

	trtTick(t, e, now, time.Date(2026, 11, 12, 5, 0, 10, 0, time.UTC))
	trtSettle(t, r, e)
	fs := trtFirings(t, r, tr.ID)
	require.Len(t, fs, 1)
	assert.Equal(t, domain.FiringSkipped, fs[0].Outcome)
	assert.Equal(t, "the factory is paused", fs[0].Reason)
	assert.Empty(t, calls.got())
	stored, err := r.st.GetTrigger(ctx, tr.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.NextRunAt)
	assert.True(t, stored.NextRunAt.Equal(time.Date(2026, 11, 13, 5, 0, 0, 0, time.UTC)))

	require.NoError(t, e.SetPaused(ctx, false))
	trtTick(t, e, now, time.Date(2026, 11, 12, 5, 1, 10, 0, time.UTC))
	trtSettle(t, r, e)
	assert.Len(t, trtFirings(t, r, tr.ID), 1, "the missed time is not replayed")
	assert.Empty(t, calls.got())
}

func TestTriggersLoopStartsNothingWhilePaused(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (*ctRig, *Engine, *time.Time, *trigHarnessCalls, domain.Trigger, domain.Trigger) {
		r, e, now, calls := trtSetup(t)
		sched := trtScheduleTrigger(t, e, "Nightly", "Sweep the issues.")
		iss := trtIssueTrigger(t, e, "Triage", "Read it.", "")
		trtIssue(r, iss.IssuesAfter.Add(time.Minute), "OWNER", "New")
		return r, e, now, calls, sched, iss
	}
	ticks := []time.Time{
		time.Date(2026, 11, 12, 5, 0, 10, 0, time.UTC),
		time.Date(2026, 11, 12, 5, 0, 40, 0, time.UTC),
		time.Date(2026, 11, 12, 5, 1, 10, 0, time.UTC),
	}
	check := func(t *testing.T, r *ctRig, calls *trigHarnessCalls, sched, iss domain.Trigger) {
		t.Helper()
		assert.Empty(t, calls.got())
		convs, err := r.st.ListConversations(ctx, 100)
		require.NoError(t, err)
		assert.Empty(t, convs)
		runs, err := r.st.ListRuns(ctx, store.RunFilter{})
		require.NoError(t, err)
		assert.Empty(t, runs)
		for _, tr := range []domain.Trigger{sched, iss} {
			fs := trtFirings(t, r, tr.ID)
			require.NotEmpty(t, fs)
			for _, f := range fs {
				assert.Equal(t, domain.FiringSkipped, f.Outcome)
			}
		}
	}

	t.Run("factory paused", func(t *testing.T) {
		r, e, now, calls, sched, iss := setup(t)
		require.NoError(t, e.SetPaused(ctx, true))
		for _, at := range ticks {
			trtTick(t, e, now, at)
		}
		trtSettle(t, r, e)
		check(t, r, calls, sched, iss)
	})

	t.Run("project paused", func(t *testing.T) {
		r, e, now, calls, sched, iss := setup(t)
		require.NoError(t, e.SetProjectPaused(ctx, "demo", true))
		for _, at := range ticks {
			trtTick(t, e, now, at)
		}
		trtSettle(t, r, e)
		check(t, r, calls, sched, iss)
		fs := trtFirings(t, r, sched.ID)
		assert.Equal(t, "project demo is paused", fs[0].Reason)

		// A manual run of the loop is allowed while paused: it records the skips.
		assert.NoError(t, e.RunLoop(ctx, loopTriggers, "demo"))
	})
}

func TestIssueSkippedWhilePausedFiresAfterResume(t *testing.T) {
	ctx := context.Background()
	r, e, now, calls := trtSetup(t)
	tr := trtIssueTrigger(t, e, "Triage", "Read it.", "")
	t0 := *tr.IssuesAfter
	n := trtIssue(r, t0.Add(time.Minute), "OWNER", "Paused")
	require.NoError(t, e.SetPaused(ctx, true))

	trtTick(t, e, now, t0.Add(2*time.Minute))
	trtTick(t, e, now, t0.Add(3*time.Minute))
	trtTick(t, e, now, t0.Add(4*time.Minute))
	trtSettle(t, r, e)
	fs := trtFirings(t, r, tr.ID)
	require.Len(t, fs, 1)
	assert.Equal(t, domain.FiringSkipped, fs[0].Outcome)
	assert.Equal(t, n, fs[0].IssueNumber)

	require.NoError(t, e.SetPaused(ctx, false))
	trtTick(t, e, now, t0.Add(5*time.Minute))
	trtSettle(t, r, e)
	fs = trtFirings(t, r, tr.ID)
	require.Len(t, fs, 2)
	assert.Equal(t, domain.FiringStarted, fs[1].Outcome)
	require.Len(t, calls.got(), 1)
	assert.Contains(t, calls.got()[0], "Paused")

	trtTick(t, e, now, t0.Add(6*time.Minute))
	trtTick(t, e, now, t0.Add(7*time.Minute))
	trtSettle(t, r, e)
	assert.Len(t, trtFirings(t, r, tr.ID), 2)
}

func TestFailedIssueFiringIsNotRetried(t *testing.T) {
	ctx := context.Background()
	r, e, now, calls := trtSetup(t)
	tr := trtIssueTrigger(t, e, "Triage", "Read it.", "")
	t0 := *tr.IssuesAfter
	n := trtIssue(r, t0.Add(time.Minute), "OWNER", "Broken")
	_, err := r.st.CreateTriggerFiring(ctx, domain.TriggerFiring{
		TriggerID: tr.ID, At: t0, Cause: domain.FiringCauseGitHubIssue, IssueNumber: n,
		Outcome: domain.FiringFailed, Reason: "disk full",
	})
	require.NoError(t, err)

	trtTick(t, e, now, t0.Add(2*time.Minute))
	trtSettle(t, r, e)
	assert.Len(t, trtFirings(t, r, tr.ID), 1)
	assert.Empty(t, calls.got())
}

func TestIssuesAreListedOnlyWhenAnIssueTriggerIsEnabled(t *testing.T) {
	ctx := context.Background()
	r, e, now, _ := trtSetup(t)
	c := &trtCountingSCM{SCM: r.fk}
	e.d.SCMFor = func(*config.Project) (scm.SCM, error) { return c, nil }

	trtScheduleTrigger(t, e, "Nightly", "Sweep the issues.")
	trtTick(t, e, now, time.Date(2026, 11, 12, 3, 1, 0, 0, time.UTC))
	assert.Equal(t, 0, c.count())

	gh, err := e.CreateTrigger(ctx, domain.Trigger{Name: "Off", Kind: domain.TriggerGitHubIssue, Project: "demo", Prompt: "p"}, "owner:alice")
	require.NoError(t, err)
	trtTick(t, e, now, time.Date(2026, 11, 12, 3, 2, 0, 0, time.UTC))
	assert.Equal(t, 0, c.count())

	_, err = e.UpdateTrigger(ctx, gh.ID, domain.TriggerPatch{Enabled: boolPtr(true)}, "owner:alice")
	require.NoError(t, err)
	trtTick(t, e, now, time.Date(2026, 11, 12, 3, 3, 0, 0, time.UTC))
	assert.Equal(t, 1, c.count())

	trtIssueTrigger(t, e, "Second", "p", "")
	trtTick(t, e, now, time.Date(2026, 11, 12, 3, 4, 0, 0, time.UTC))
	assert.Equal(t, 2, c.count(), "two enabled issue triggers still list once per tick")
}

func TestIssueListingFailureIsReportedAndSchedulesStillFire(t *testing.T) {
	r, e, now, _ := trtSetup(t)
	c := &trtCountingSCM{SCM: r.fk, err: errors.New("github is down")}
	e.d.SCMFor = func(*config.Project) (scm.SCM, error) { return c, nil }
	sched := trtScheduleTrigger(t, e, "Nightly", "Sweep the issues.")
	iss := trtIssueTrigger(t, e, "Triage", "Read it.", "")

	*now = time.Date(2026, 11, 12, 5, 0, 10, 0, time.UTC)
	err := e.runLoopOnce(context.Background(), loopTriggers, "demo", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list opened issues")
	trtSettle(t, r, e)

	assert.Len(t, trtFirings(t, r, sched.ID), 1)
	assert.Empty(t, trtFirings(t, r, iss.ID))
}

func TestTriggersLoopIsListed(t *testing.T) {
	_, e, _, _ := trtSetup(t)
	var rows []api.LoopView
	for _, v := range e.Loops(context.Background()) {
		if v.Name == loopTriggers && v.Project == "demo" {
			rows = append(rows, v)
		}
	}
	require.Len(t, rows, 1)
	assert.True(t, rows[0].Enabled)
	assert.Equal(t, "30s", rows[0].Every)
}

func boolPtr(b bool) *bool { return &b }
