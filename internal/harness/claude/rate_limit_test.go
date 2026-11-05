package claude

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/harness"
)

// replayAt is replay with the parser's clock fixed.
func replayAt(t *testing.T, path string, now time.Time) harness.Result {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	p := newParser("")
	p.now = func() time.Time { return now }
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		p.parseLine(line)
	}
	return p.result(1, nil)
}

func TestRateLimitHitWithEpochReset(t *testing.T) {
	res := replayAt(t, "testdata/rate_limit_epoch.jsonl", time.Unix(1893450000, 0))
	require.NotNil(t, res.RateLimit)
	assert.True(t, res.RateLimit.ResetAt.Equal(time.Unix(1893456000, 0)), "got %v", res.RateLimit.ResetAt)
	assert.Contains(t, res.RateLimit.Message, "usage limit reached")
}

func TestRateLimitHitWithClockTimeReset(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	res := replayAt(t, "testdata/rate_limit_clock.jsonl", now)
	require.NotNil(t, res.RateLimit)
	assert.True(t, res.RateLimit.ResetAt.Equal(time.Date(2026, 11, 4, 15, 0, 0, 0, time.Local)), "got %v", res.RateLimit.ResetAt)
}

func TestRateLimitHitWithoutResetTime(t *testing.T) {
	p := newParser("")
	p.parseLine(`{"type":"result","is_error":true,"api_error_status":429,"result":"API Error: 429 rate limited"}`)
	res := p.result(1, nil)
	require.NotNil(t, res.RateLimit)
	assert.True(t, res.RateLimit.ResetAt.IsZero())
}

func TestRateLimitWarningDuringSuccessfulRunIsNotAHit(t *testing.T) {
	_, res := replay(t, "testdata/rate_limit_warning.jsonl")
	assert.False(t, res.IsError)
	assert.Nil(t, res.RateLimit)
}

func TestRejectedEventSuppliesResetWhenTextHasNone(t *testing.T) {
	p := newParser("")
	p.now = func() time.Time { return time.Unix(1893450000, 0) }
	p.parseLine(`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1893456000}}`)
	p.parseLine(`{"type":"result","is_error":true,"result":"limit hit"}`)
	res := p.result(1, nil)
	require.NotNil(t, res.RateLimit)
	assert.True(t, res.RateLimit.ResetAt.Equal(time.Unix(1893456000, 0)))
}

func TestOrdinaryErrorIsNotARateLimit(t *testing.T) {
	p := newParser("")
	p.parseLine(`{"type":"result","is_error":true,"result":"tool crashed"}`)
	assert.Nil(t, p.result(1, nil).RateLimit)
}
