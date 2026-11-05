package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
)

// rtStoreBilledCost records a run with a cost and a billing mode, started now.
func rtStoreBilledCost(t *testing.T, env *rtEnv, cost float64, billing string) {
	t.Helper()
	_, err := env.st.CreateRun(env.ctx, domain.Run{TaskID: "earlier", Project: "demo",
		Status: domain.RunSucceeded, CostUSD: cost, Billing: billing, StartedAt: time.Now()})
	require.NoError(t, err)
}

// Subscription runs do not count toward the daily budget; metered and unknown runs do.
func TestDailyBudgetCountsOnlyNonSubscriptionRuns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		billing string
		within  bool
	}{
		{harness.BillingSubscription, true},
		{harness.BillingMetered, false},
		{"", false},
	} {
		t.Run("billing="+tc.billing, func(t *testing.T) {
			env := rtNewEngine(t, rtOpts{})
			rtStoreBilledCost(t, env, 5, tc.billing)
			assert.Equal(t, tc.within, env.e.withinDailyBudget(env.ctx, 1.0))
		})
	}
}

// A run records the harness's detected billing, and a non-auto config value overrides it.
func TestSuperviseRecordsBilling(t *testing.T) {
	for _, tc := range []struct{ detected, override, want string }{
		{harness.BillingSubscription, "", harness.BillingSubscription},
		{"", "", ""},
		{harness.BillingSubscription, harness.BillingMetered, harness.BillingMetered},
		{harness.BillingMetered, "auto", harness.BillingMetered},
		{"", harness.BillingSubscription, harness.BillingSubscription},
	} {
		t.Run(tc.detected+"/"+tc.override, func(t *testing.T) {
			env := rtNewEngine(t, rtOpts{
				script: costScript(harness.Result{CostUSD: 1, Billing: tc.detected}),
				editConfig: func(cfg *config.Config) {
					hs := map[string]config.Harness{}
					for k, v := range cfg.Harness {
						v.Billing = tc.override
						hs[k] = v
					}
					cfg.Harness = hs
				},
			})

			task := env.create(domain.Task{Body: "billing"})
			runs := env.firstEndedRun(task.ID)
			assert.Equal(t, tc.want, runs[0].Billing)
		})
	}
}
