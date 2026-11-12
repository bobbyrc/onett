package engine

import (
	"context"
	"sync/atomic"
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

// rtInFlightPrice prices the fake harness so that a running run's prompt tokens become dollars:
// cached input, the rate a run's cost so far is counted at, is $1 per million tokens.
const rtInFlightPrice = "input_per_mtok: 2, output_per_mtok: 10, cached_input_per_mtok: 1"

// rtInFlightScript is a run that reports the prompt size of each of its model requests, then stays
// in flight until release is closed (it then ends with res) or it is cancelled. A nil release
// never ends it.
func rtInFlightScript(prompts []int64, release <-chan struct{}, res harness.Result) hfake.Script {
	return func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		for _, n := range prompts {
			emit(harness.Event{ContextTokens: n})
		}
		select {
		case <-release:
			return res, nil
		case <-ctx.Done():
			return harness.Result{}, ctx.Err()
		}
	}
}

// rtBudgetConfig sets the daily budget, and a stall timeout long enough that a quiet run in flight
// is not stopped as stalled during the test.
func rtBudgetConfig(budget float64) func(*config.Config) {
	return func(cfg *config.Config) {
		cfg.Limits.DailyBudgetUSD = budget
		cfg.Limits.StallTimeout = config.Duration{Duration: time.Minute}
	}
}

// rtMidnight is the start of the local day, which is where the daily budget starts counting.
func rtMidnight() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// Two long runs in flight whose cost so far reaches the budget stop a third task from starting.
func TestRtDailyBudgetCountsRunsInFlight(t *testing.T) {
	t.Parallel()
	env := rtNewEngine(t, rtOpts{
		projects:   []string{"demo", "other", "third"},
		fakePrice:  rtInFlightPrice,
		script:     rtInFlightScript([]int64{600_000}, nil, harness.Result{}),
		editConfig: rtBudgetConfig(1.0),
	})
	a := env.create(domain.Task{Project: "demo", Body: "long"})
	b := env.create(domain.Task{Project: "other", Body: "long"})
	for _, id := range []string{a.ID, b.ID} {
		rtWait(t, 20*time.Second, "run to report its prompt size", func() bool {
			runs := env.runs(id)
			return len(runs) == 1 && runs[0].ContextTokens == 600_000
		})
	}

	// Each run has cost $0.60 so far: $1.20 against a budget of $1.00.
	c := env.create(domain.Task{Project: "third", Body: "long"})
	for i := 0; i < 5; i++ {
		env.e.schedule(env.ctx)
	}
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, domain.TaskQueued, env.task(c.ID).Status, "the runs in flight reached the budget: nothing new starts")
	assert.Empty(t, env.runs(c.ID))
	assert.Equal(t, 1, rtBudgetEvents(env), "one budget event")
}

// A run's cost so far is on its run row while it is in flight, and the cost it reports when it
// ends replaces it: the run counts once, not twice.
func TestRtRunCostCountsOnceWhenItEnds(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	env := rtNewEngine(t, rtOpts{
		maxAttempts: 1,
		fakePrice:   rtInFlightPrice,
		// The second 200k repeats the first request, as an adapter may report one request more
		// than once; it is counted once.
		script:     rtInFlightScript([]int64{200_000, 200_000, 400_000}, release, harness.Result{CostUSD: 0.7}),
		editConfig: rtBudgetConfig(0),
	})
	task := env.create(domain.Task{Body: "long"})
	rtWait(t, 20*time.Second, "run to report its last prompt size", func() bool {
		runs := env.runs(task.ID)
		return len(runs) == 1 && runs[0].ContextTokens == 400_000
	})

	live := env.runs(task.ID)[0]
	assert.Nil(t, live.EndedAt, "still in flight")
	assert.InDelta(t, 0.6, live.CostUSD, 1e-9, "600k prompt tokens so far at $1 per million")
	assert.True(t, live.CostEstimated)
	today, err := env.st.CostSince(env.ctx, rtMidnight())
	require.NoError(t, err)
	assert.InDelta(t, 0.6, today, 1e-9, "the budget sees the run in flight")

	close(release)
	ended := env.firstEndedRun(task.ID)[0]
	assert.Equal(t, live.ID, ended.ID)
	assert.InDelta(t, 0.7, ended.CostUSD, 1e-9, "the reported cost replaces the estimate")
	assert.False(t, ended.CostEstimated)
	today, err = env.st.CostSince(env.ctx, rtMidnight())
	require.NoError(t, err)
	assert.InDelta(t, 0.7, today, 1e-9, "counted once, at the reported cost")
}

// A run that is stopped before it reports a cost keeps its cost so far: the money was spent.
func TestRtStoppedRunKeepsCostSoFar(t *testing.T) {
	t.Parallel()
	env := rtNewEngine(t, rtOpts{ // stall_timeout is 2s
		maxAttempts: 1,
		fakePrice:   rtInFlightPrice,
		script:      rtInFlightScript([]int64{600_000}, nil, harness.Result{}),
	})
	task := env.create(domain.Task{Body: "long"})
	ended := env.firstEndedRun(task.ID)[0]
	assert.Equal(t, domain.RunStalled, ended.Status)
	assert.InDelta(t, 0.6, ended.CostUSD, 1e-9)
	assert.True(t, ended.CostEstimated)
}

// A run in flight gets no cost so far when the estimate could be too high or does not count: a
// price without a cached-input rate, or a harness set to subscription billing.
func TestRtNoCostSoFarWithoutCachedRateOrOnSubscription(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, price, billing string
	}{
		{"no cached rate", "input_per_mtok: 2, output_per_mtok: 10", ""},
		{"subscription harness", rtInFlightPrice, harness.BillingSubscription},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := rtNewEngine(t, rtOpts{
				maxAttempts: 1,
				fakePrice:   tc.price,
				script:      rtInFlightScript([]int64{600_000}, nil, harness.Result{}),
				editConfig: func(cfg *config.Config) {
					rtBudgetConfig(0)(cfg)
					hs := map[string]config.Harness{}
					for k, v := range cfg.Harness {
						v.Billing = tc.billing
						hs[k] = v
					}
					cfg.Harness = hs
				},
			})
			task := env.create(domain.Task{Body: "long"})
			rtWait(t, 20*time.Second, "run to report its prompt size", func() bool {
				runs := env.runs(task.ID)
				return len(runs) == 1 && runs[0].ContextTokens == 600_000
			})
			live := env.runs(task.ID)[0]
			assert.Zero(t, live.CostUSD)
			assert.False(t, live.CostEstimated)
		})
	}
}

// rtSpendOnClaimStore is a store in which starting a run spends the whole budget: today's cost is
// 0 until a task has been claimed, and 100 after.
type rtSpendOnClaimStore struct {
	store.Store
	claimed *atomic.Bool
}

func (s rtSpendOnClaimStore) ClaimNextTask(ctx context.Context, busy []string, held ...store.ProjectRole) (domain.Task, error) {
	t, err := s.Store.ClaimNextTask(ctx, busy, held...)
	if err == nil {
		s.claimed.Store(true)
	}
	return t, err
}

func (s rtSpendOnClaimStore) CostSince(context.Context, time.Time) (float64, error) {
	if s.claimed.Load() {
		return 100, nil
	}
	return 0, nil
}

// The budget is checked before every claim of a scheduler pass, not once per pass: when the first
// run started spends it, the second queued task is not started in the same pass.
func TestRtDailyBudgetCheckedBeforeEveryClaim(t *testing.T) {
	t.Parallel()
	claimed := &atomic.Bool{}
	env := rtNewEngine(t, rtOpts{
		projects: []string{"demo", "other"},
		script:   rtInFlightScript(nil, nil, harness.Result{}),
		wrapStore: func(s store.Store) store.Store {
			return rtSpendOnClaimStore{Store: s, claimed: claimed}
		},
		editConfig: rtBudgetConfig(1.0),
	})
	// Both tasks are queued while the factory is paused, so one scheduler pass sees both.
	require.NoError(t, env.e.SetPaused(env.ctx, true))
	a := env.create(domain.Task{Project: "demo", Body: "long"})
	b := env.create(domain.Task{Project: "other", Body: "long"})
	require.NoError(t, env.e.SetPaused(env.ctx, false))
	env.e.wakeScheduler()
	started := func() int { return len(env.runs(a.ID)) + len(env.runs(b.ID)) }
	rtWait(t, 20*time.Second, "one task to start", func() bool { return started() > 0 })

	for i := 0; i < 5; i++ {
		env.e.schedule(env.ctx)
	}
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 1, started(), "the first run spent the budget: the second task waits")
}
