package engine

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
	"github.com/bobbyrc/onett/internal/store"
)

func TestHoldUsesResetTimePlusMargin(t *testing.T) {
	var tbl holdTable
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	reset := now.Add(37 * time.Minute)
	h := tbl.hit("claude", harness.RateLimit{ResetAt: reset, Message: "limit"}, now)
	assert.Equal(t, reset.Add(holdMargin), h.Until)
	assert.Len(t, tbl.active(now), 1)
	assert.Empty(t, tbl.active(h.Until), "the hold lifts at Until")
	_, held := tbl.held("codex", now)
	assert.False(t, held, "other harnesses are not held")
}

func TestHoldBacksOffWithoutResetTime(t *testing.T) {
	var tbl holdTable
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	var got []time.Duration
	for range 6 {
		h := tbl.hit("codex", harness.RateLimit{}, now)
		got = append(got, h.Until.Sub(now))
	}
	assert.Equal(t, []time.Duration{
		5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute, time.Hour, time.Hour,
	}, got)

	tbl.clear("codex")
	assert.Equal(t, 5*time.Minute, tbl.hit("codex", harness.RateLimit{}, now).Until.Sub(now),
		"a run that finished without a limit resets the backoff")
}

func TestHoldIgnoresResetTimeInThePast(t *testing.T) {
	var tbl holdTable
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	h := tbl.hit("claude", harness.RateLimit{ResetAt: now.Add(-time.Minute)}, now)
	assert.Equal(t, now.Add(holdBackoffStart), h.Until)
}

// limitOnce hits the rate limit on its first run, with a reset at resetAt, and behaves like a
// no-op agent after that.
func limitOnce(resetAt time.Time, calls *atomic.Int32) hfake.Script {
	return func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		if calls.Add(1) == 1 {
			return harness.Result{IsError: true, RateLimit: &harness.RateLimit{
				ResetAt: resetAt, Message: "usage limit reached",
			}}, nil
		}
		return hfake.DefaultScript(ctx, spec, emit)
	}
}

func TestRateLimitedRunHoldsHarnessAndResumes(t *testing.T) {
	old := holdMargin
	holdMargin = 0
	t.Cleanup(func() { holdMargin = old })
	// The engine's clock is the test's, so the hold lasts until the test ends it. With a hold of
	// real seconds, a loaded machine let it lapse before the "still held" checks below had run.
	clock := &fakeClock{t: time.Now()}
	resetAt := clock.Now().Add(time.Hour)

	var calls atomic.Int32
	env := rtNewEngine(t, rtOpts{
		projects:    []string{"demo", "other"},
		maxAttempts: 1, // a hit that used up an attempt would block the task
		projectExtra: map[string]string{
			"other": "    agents:\n      implementer: {harness: alt, prompt: builtin:implementer}\n",
		},
		scriptFor: map[string]hfake.Script{"fake": limitOnce(resetAt, &calls), "alt": hfake.DefaultScript},
		now:       clock.Now,
	})

	held := env.create(domain.Task{Project: "demo", Title: "Held task", Body: "no-op"})
	rtWait(t, 10*time.Second, "the harness to be held", func() bool { return len(env.e.Holds()) == 1 })

	assert.Equal(t, "fake", env.e.Holds()[0].Harness)
	// The hold is recorded before the run ends and the task is requeued, so wait for that too.
	rtWait(t, 10*time.Second, "the task to be requeued", func() bool {
		return env.task(held.ID).Status == domain.TaskQueued
	})
	got := env.task(held.ID)
	assert.Equal(t, domain.TaskQueued, got.Status, "the task waits instead of failing or blocking")
	assert.Equal(t, 0, got.Attempts, "the hit does not use an attempt")
	runs := env.runs(held.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, domain.RunCancelled, runs[0].Status)
	assert.Contains(t, runs[0].Error, "rate limited")

	// A task on another harness keeps running while the first harness is held.
	other := env.create(domain.Task{Project: "other", Title: "Other task", Body: "no-op"})
	env.waitStatus(other.ID, domain.TaskDone)
	assert.Equal(t, domain.TaskQueued, env.task(held.ID).Status, "still held")
	assert.EqualValues(t, 1, calls.Load(), "no run started on the held harness")

	// When the hold expires the task runs on its own.
	clock.set(resetAt.Add(time.Second))
	done := env.waitStatus(held.ID, domain.TaskDone)
	assert.Equal(t, 1, done.Attempts)
	assert.EqualValues(t, 2, calls.Load())
	assert.Empty(t, env.e.Holds())

	evs, err := env.st.ListEvents(env.ctx, store.EventFilter{TaskID: held.ID, FromStart: true})
	require.NoError(t, err)
	var seen bool
	for _, ev := range evs {
		if strings.Contains(ev.Message, "Held task: held: fake rate limit, resets") {
			seen = true
		}
	}
	assert.True(t, seen, "the task has a held event")
}
