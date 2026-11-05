package codex

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/onett/internal/harness"
)

func TestParseLoginStatus(t *testing.T) {
	assert.Equal(t, harness.BillingSubscription, parseLoginStatus("Logged in using ChatGPT\n"))
	assert.Equal(t, harness.BillingMetered, parseLoginStatus("Logged in using an API key - sk-***\n"))
	assert.Equal(t, "", parseLoginStatus("Not logged in\n"))
	assert.Equal(t, "", parseLoginStatus(""))
}

func newBillingRunner(cfg harness.Config, out string, err error, calls *int) (*runner, *time.Time) {
	now := time.Unix(1000, 0)
	r := &runner{cfg: cfg, now: func() time.Time { return now }}
	r.loginStatus = func(_ context.Context, args []string, _ harness.Spec) (string, error) {
		*calls++
		return out, err
	}
	return r, &now
}

func TestBillingDetectionCachesFiveMinutes(t *testing.T) {
	calls := 0
	r, now := newBillingRunner(harness.Config{}, "Logged in using ChatGPT", nil, &calls)
	assert.Equal(t, harness.BillingSubscription, r.billing(harness.Spec{}))
	assert.Equal(t, harness.BillingSubscription, r.billing(harness.Spec{}))
	assert.Equal(t, 1, calls, "second call served from cache")
	*now = now.Add(billingCacheTTL + time.Second)
	r.billing(harness.Spec{})
	assert.Equal(t, 2, calls, "cache expired")
}

func TestBillingFailureIsUnknown(t *testing.T) {
	calls := 0
	r, _ := newBillingRunner(harness.Config{}, "Logged in using ChatGPT", errors.New("boom"), &calls)
	assert.Equal(t, "", r.billing(harness.Spec{}))
}

func TestBillingConfigOverrideSkipsDetection(t *testing.T) {
	calls := 0
	r, _ := newBillingRunner(harness.Config{Billing: harness.BillingMetered}, "Logged in using ChatGPT", nil, &calls)
	assert.Equal(t, harness.BillingMetered, r.billing(harness.Spec{}))
	assert.Zero(t, calls)
}

func TestLoginStatusArgsUsesConfiguredCommand(t *testing.T) {
	assert.Equal(t, []string{"codex", "login", "status"}, loginStatusArgs(harness.Config{}))
	assert.Equal(t, []string{"/x/codex", "--a", "login", "status"}, loginStatusArgs(harness.Config{Command: []string{"/x/codex", "--a"}}))
}
