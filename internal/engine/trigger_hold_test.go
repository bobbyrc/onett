package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
)

// hldHold puts a rate-limit hold on the orchestrator's harness that lasts until two hours after
// at, and returns the harness name.
func hldHold(t *testing.T, e *Engine, at time.Time) string {
	t.Helper()
	_, agent, err := e.harnessFor("demo", chatAgentName)
	require.NoError(t, err)
	// The hold lasts until ResetAt plus holdMargin, so back ResetAt off by the margin.
	e.noteRateLimit(context.Background(), agent.Harness, harness.RateLimit{
		ResetAt: at.Add(2*time.Hour - holdMargin), Message: "limit",
	})
	return agent.Harness
}

func TestIssueHeldByRateLimitIsRecordedAsSkippedOnce(t *testing.T) {
	r, e, now, calls := trtSetup(t)
	start := *now
	tr := trtIssueTrigger(t, e, "Triage", "Read it.", "")
	trtIssue(r, tr.IssuesAfter.Add(time.Minute), "OWNER", "Crash")
	harnessName := hldHold(t, e, start)

	for i := 1; i <= 5; i++ {
		trtTick(t, e, now, start.Add(time.Duration(3*i)*time.Minute))
	}
	trtSettle(t, r, e)

	fs := trtFirings(t, r, tr.ID)
	require.Len(t, fs, 1, "a pending issue is recorded as skipped once, not once per tick")
	assert.Equal(t, domain.FiringSkipped, fs[0].Outcome)
	assert.True(t, strings.HasPrefix(fs[0].Reason, harnessName+" held: rate limit, resets "), fs[0].Reason)
	assert.NotContains(t, fs[0].Reason, "left")
	assert.Empty(t, calls.got(), "the held harness is never called")

	// The hold ends: the still-pending issue starts a turn.
	trtTick(t, e, now, start.Add(2*time.Hour+time.Minute))
	trtSettle(t, r, e)
	fs = trtFirings(t, r, tr.ID)
	require.Len(t, fs, 2)
	assert.Equal(t, domain.FiringStarted, fs[1].Outcome)
}

func TestRunTriggerWhileHarnessHeldIsSkipped(t *testing.T) {
	_, e, now, calls := trtSetup(t)
	tr := trtScheduleTrigger(t, e, "Sweep", "Sweep.")
	harnessName := hldHold(t, e, *now)

	f, err := e.RunTrigger(context.Background(), tr.ID, "owner:alice")
	require.NoError(t, err)
	assert.Equal(t, domain.FiringSkipped, f.Outcome)
	assert.True(t, strings.HasPrefix(f.Reason, harnessName+" held: rate limit, resets "), f.Reason)
	assert.Empty(t, calls.got(), "the held harness is never called")
}
