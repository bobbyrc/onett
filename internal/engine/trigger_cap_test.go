package engine

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
	"github.com/bobbyrc/onett/internal/store"
)

const capCapReason = "this trigger already fired 20 times in the last hour"

// capSpecs records the harness.Spec of every turn the fake harness is started with.
type capSpecs struct {
	mu    sync.Mutex
	specs []harness.Spec
}

func (c *capSpecs) harness() harness.Harness {
	return hfake.New(func(_ context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
		c.mu.Lock()
		c.specs = append(c.specs, spec)
		c.mu.Unlock()
		return harness.Result{SessionID: "s", FinalText: "done"}, nil
	})
}

func (c *capSpecs) got() []harness.Spec {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]harness.Spec(nil), c.specs...)
}

// capSetup is trigRig with a spec-recording harness. repo, when set, is demo's repository.
func capSetup(t *testing.T, repo string) (*ctRig, *Engine, *time.Time, *capSpecs) {
	t.Helper()
	r := ctNewRig(t)
	if repo != "" {
		r.cfg.Projects["demo"].Repo = repo
	}
	e := r.engine(nil)
	now := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	e.d.Now = func() time.Time { return now }
	specs := &capSpecs{}
	e.d.HarnessFor = func(string, config.Harness) (harness.Harness, error) { return specs.harness(), nil }
	return r, e, &now, specs
}

// capSpread returns n times evenly spaced from first to last, inclusive.
func capSpread(first, last time.Time, n int) []time.Time {
	out := make([]time.Time, n)
	for i := range out {
		out[i] = first.Add(last.Sub(first) * time.Duration(i) / time.Duration(n-1))
	}
	return out
}

// capSeed stores one firing per time, with the given outcome, for trigger id.
func capSeed(t *testing.T, r *ctRig, id string, at []time.Time, outcome string) {
	t.Helper()
	for _, a := range at {
		_, err := r.st.CreateTriggerFiring(context.Background(), domain.TriggerFiring{
			TriggerID: id, At: a, Cause: domain.FiringCauseManual, Outcome: outcome,
		})
		require.NoError(t, err)
	}
}

// capNewestRun returns the most recently started run of conversation convID.
func capNewestRun(t *testing.T, r *ctRig, convID string) domain.Run {
	t.Helper()
	runs, err := r.st.ListRuns(context.Background(), store.RunFilter{})
	require.NoError(t, err)
	var newest domain.Run
	for _, run := range runs {
		if run.ConversationID == convID && run.StartedAt.After(newest.StartedAt) {
			newest = run
		}
	}
	require.NotEmpty(t, newest.ID, "a run exists for the conversation")
	return newest
}

func TestTriggerHourlyCapSkipsFurtherFirings(t *testing.T) {
	ctx := context.Background()
	r, e, now, specs := capSetup(t, "")
	tr, err := e.CreateTrigger(ctx, domain.Trigger{Name: "Sweep", Kind: domain.TriggerSchedule, Project: "demo", Prompt: "Sweep.", Cron: "0 5 * * *", TimeZone: "UTC"}, "owner:alice")
	require.NoError(t, err)
	capSeed(t, r, tr.ID, capSpread(now.Add(-50*time.Minute), now.Add(-time.Minute), 20), domain.FiringStarted)

	f, err := e.RunTrigger(ctx, tr.ID, "owner:alice")
	require.NoError(t, err)
	assert.Equal(t, domain.FiringSkipped, f.Outcome)
	assert.Equal(t, capCapReason, f.Reason)
	assert.Empty(t, specs.got(), "a capped firing starts no turn")

	// Fifteen minutes on, the oldest seeded firing is outside the hour, so 19 remain inside it.
	*now = now.Add(15 * time.Minute)
	f, err = e.RunTrigger(ctx, tr.ID, "owner:alice")
	require.NoError(t, err)
	assert.Equal(t, domain.FiringStarted, f.Outcome)
	trigIdle(t, r, e, f.ConversationID)
	assert.Len(t, specs.got(), 1)
}

func TestOnlyStartedFiringsCountTowardTheCap(t *testing.T) {
	ctx := context.Background()
	r, e, now, specs := capSetup(t, "")
	tr, err := e.CreateTrigger(ctx, domain.Trigger{Name: "Sweep", Kind: domain.TriggerSchedule, Project: "demo", Prompt: "Sweep.", Cron: "0 5 * * *", TimeZone: "UTC"}, "owner:alice")
	require.NoError(t, err)
	capSeed(t, r, tr.ID, capSpread(now.Add(-50*time.Minute), now.Add(-time.Minute), 19), domain.FiringStarted)
	capSeed(t, r, tr.ID, capSpread(now.Add(-50*time.Minute), now.Add(-time.Minute), 30), domain.FiringSkipped)
	capSeed(t, r, tr.ID, capSpread(now.Add(-50*time.Minute), now.Add(-time.Minute), 5), domain.FiringFailed)

	f, err := e.RunTrigger(ctx, tr.ID, "owner:alice")
	require.NoError(t, err)
	assert.Equal(t, domain.FiringStarted, f.Outcome)
	trigIdle(t, r, e, f.ConversationID)
	assert.Len(t, specs.got(), 1)
}

func TestScheduleOverTheHourlyCapIsSkippedAndItsTimeIsGone(t *testing.T) {
	r, e, now, specs := capSetup(t, "")
	tr := trtScheduleTrigger(t, e, "Nightly", "Sweep.")
	capSeed(t, r, tr.ID, capSpread(time.Date(2026, 11, 12, 4, 10, 0, 0, time.UTC), time.Date(2026, 11, 12, 4, 59, 0, 0, time.UTC), 20), domain.FiringStarted)

	trtTick(t, e, now, time.Date(2026, 11, 12, 5, 0, 0, 0, time.UTC))

	fs := trtFirings(t, r, tr.ID)
	require.Len(t, fs, 21)
	last := fs[len(fs)-1]
	assert.Equal(t, domain.FiringCauseSchedule, last.Cause)
	assert.Equal(t, domain.FiringSkipped, last.Outcome)
	assert.Equal(t, capCapReason, last.Reason)
	assert.Empty(t, specs.got(), "a capped schedule starts no turn")

	stored, err := r.st.GetTrigger(context.Background(), tr.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.NextRunAt)
	assert.True(t, stored.NextRunAt.Equal(time.Date(2026, 11, 13, 5, 0, 0, 0, time.UTC)), "next run is tomorrow at 05:00, got %s", stored.NextRunAt)
}

func TestIssueOverTheHourlyCapStaysPending(t *testing.T) {
	r, e, now, specs := capSetup(t, "o/r")
	tr := trtIssueTrigger(t, e, "Triage", "Triage the issue.", "")
	seeds := capSpread(now.Add(-50*time.Minute), now.Add(-time.Minute), 20)
	for i, at := range seeds {
		_, err := r.st.CreateTriggerFiring(context.Background(), domain.TriggerFiring{
			TriggerID: tr.ID, At: at, Cause: domain.FiringCauseGitHubIssue, IssueNumber: 1001 + i,
			Outcome: domain.FiringStarted,
		})
		require.NoError(t, err)
	}
	number := trtIssue(r, now.Add(30*time.Minute), "OWNER", "A new issue")

	base := *now
	trtTick(t, e, now, base.Add(1*time.Second))
	trtTick(t, e, now, base.Add(2*time.Second))
	trtTick(t, e, now, base.Add(3*time.Second))

	forIssue := func() []domain.TriggerFiring {
		var out []domain.TriggerFiring
		for _, f := range trtFirings(t, r, tr.ID) {
			if f.IssueNumber == number {
				out = append(out, f)
			}
		}
		return out
	}
	got := forIssue()
	require.Len(t, got, 1, "the issue is recorded as skipped once, not once per tick")
	assert.Equal(t, domain.FiringSkipped, got[0].Outcome)
	assert.Equal(t, capCapReason, got[0].Reason)
	assert.Empty(t, specs.got())

	trtTick(t, e, now, base.Add(61*time.Minute))
	trtSettle(t, r, e)
	got = forIssue()
	require.Len(t, got, 2)
	assert.Equal(t, domain.FiringStarted, got[1].Outcome)
	assert.Len(t, specs.got(), 1, "the issue fires once the hour has passed")

	trtTick(t, e, now, base.Add(61*time.Minute+time.Second))
	trtSettle(t, r, e)
	assert.Len(t, forIssue(), 2, "a settled issue does not fire again")
	assert.Len(t, specs.got(), 1)
}

func TestTriggerConversationHasKindTrigger(t *testing.T) {
	ctx := context.Background()
	r, e, _, _ := capSetup(t, "")
	tr, err := e.CreateTrigger(ctx, domain.Trigger{Name: "Sweep", Kind: domain.TriggerSchedule, Project: "demo", Prompt: "Sweep.", Cron: "0 5 * * *", TimeZone: "UTC"}, "owner:alice")
	require.NoError(t, err)
	f, err := e.RunTrigger(ctx, tr.ID, "owner:alice")
	require.NoError(t, err)
	trigIdle(t, r, e, f.ConversationID)

	conv, err := r.st.GetConversation(ctx, f.ConversationID)
	require.NoError(t, err)
	assert.Equal(t, domain.ConversationTrigger, conv.Kind)
	b, err := json.Marshal(conv)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"kind":"trigger"`)
}

func TestTriggerTurnsIgnoreStoredConversationSettings(t *testing.T) {
	ctx := context.Background()
	r, e, _, specs := capSetup(t, "")
	_, agent, err := e.harnessFor("demo", chatAgentName)
	require.NoError(t, err)
	permissive := domain.ConversationSettings{Model: "someone-chose-this", Effort: "max", PermissionMode: "bypassPermissions"}
	require.NotEqual(t, agent.Model, permissive.Model)
	require.NotEqual(t, agent.Effort, permissive.Effort)
	require.NotEqual(t, agent.PermissionMode, permissive.PermissionMode)

	tr, err := e.CreateTrigger(ctx, domain.Trigger{Name: "Sweep", Kind: domain.TriggerSchedule, Project: "demo", Prompt: "Sweep.", Cron: "0 5 * * *", TimeZone: "UTC"}, "owner:alice")
	require.NoError(t, err)

	// a: the first firing creates the conversation.
	f, err := e.RunTrigger(ctx, tr.ID, "owner:alice")
	require.NoError(t, err)
	convID := f.ConversationID
	trigIdle(t, r, e, convID)

	// b: someone stores permissive settings on the trigger's conversation.
	_, err = r.st.UpdateConversation(ctx, convID, func(c *domain.Conversation) error {
		c.Settings = permissive
		return nil
	})
	require.NoError(t, err)

	// c: a firing after that ignores them.
	f, err = e.RunTrigger(ctx, tr.ID, "owner:alice")
	require.NoError(t, err)
	trigIdle(t, r, e, convID)
	all := specs.got()
	last := all[len(all)-1]
	assert.Equal(t, agent.Model, last.Model)
	assert.Equal(t, agent.Effort, last.Effort)
	assert.Equal(t, agent.PermissionMode, last.PermissionMode)
	run := capNewestRun(t, r, convID)
	assert.Equal(t, agent.Model, run.Model)
	assert.Equal(t, agent.Effort, run.Effort)
	assert.Equal(t, agent.PermissionMode, run.PermissionMode)

	// d: a person's message in the same conversation also gets the defaults.
	_, err = e.SendChat(ctx, convID, "hello")
	require.NoError(t, err)
	trigIdle(t, r, e, convID)
	all = specs.got()
	last = all[len(all)-1]
	assert.Equal(t, agent.Model, last.Model)
	assert.Equal(t, agent.Effort, last.Effort)
	assert.Equal(t, agent.PermissionMode, last.PermissionMode)

	// e: control. An ordinary conversation with the same stored settings does use them.
	ordinary, err := r.st.CreateConversation(ctx, domain.Conversation{Project: "demo"})
	require.NoError(t, err)
	_, err = r.st.UpdateConversation(ctx, ordinary.ID, func(c *domain.Conversation) error {
		c.Settings = permissive
		return nil
	})
	require.NoError(t, err)
	_, err = e.SendChat(ctx, ordinary.ID, "hello")
	require.NoError(t, err)
	trigIdle(t, r, e, ordinary.ID)
	all = specs.got()
	last = all[len(all)-1]
	assert.Equal(t, permissive.Model, last.Model)
	assert.Equal(t, permissive.Effort, last.Effort)
	assert.Equal(t, permissive.PermissionMode, last.PermissionMode)
}
