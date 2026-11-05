package claude

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/onett/internal/harness"
)

func TestBillingFromAPIKeySource(t *testing.T) {
	for _, tc := range []struct{ name, init, want string }{
		{"subscription", `{"type":"system","subtype":"init","session_id":"s","apiKeySource":"none"}`, harness.BillingSubscription},
		{"env key", `{"type":"system","subtype":"init","session_id":"s","apiKeySource":"ANTHROPIC_API_KEY"}`, harness.BillingMetered},
		{"helper", `{"type":"system","subtype":"init","session_id":"s","apiKeySource":"apiKeyHelper"}`, harness.BillingMetered},
		{"missing field", `{"type":"system","subtype":"init","session_id":"s"}`, ""},
		{"no init line", `{"type":"assistant","message":{"content":[]}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newParser("")
			p.parseLine(tc.init)
			assert.Equal(t, tc.want, p.result(0, nil).Billing)
		})
	}
}
