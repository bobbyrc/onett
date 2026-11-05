package codex

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func replayFile(t *testing.T, path string) (resultRateLimitAt time.Time, hit bool, msg string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	p := newParser()
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		p.parseLine(line)
	}
	res := p.result(1, nil)
	if res.RateLimit == nil {
		return time.Time{}, false, ""
	}
	return res.RateLimit.ResetAt, true, res.RateLimit.Message
}

func TestUsageLimitWithRetryDelay(t *testing.T) {
	before := time.Now()
	at, hit, msg := replayFile(t, "testdata/usage_limit.jsonl")
	require.True(t, hit)
	assert.Contains(t, msg, "usage limit")
	assert.WithinDuration(t, before.Add(2*time.Hour+5*time.Minute), at, 5*time.Second)
}

func TestRateLimit429WithoutResetTime(t *testing.T) {
	at, hit, _ := replayFile(t, "testdata/rate_limit_no_time.jsonl")
	require.True(t, hit)
	assert.True(t, at.IsZero())
}

func TestReconnectingErrorIsNotARateLimit(t *testing.T) {
	p := newParser()
	p.parseLine(lineError)
	assert.Nil(t, p.result(1, nil).RateLimit)
}

func TestRecoveredLimitErrorIsNotAHit(t *testing.T) {
	p := newParser()
	p.parseLine(`{"type":"error","message":"rate limit, retrying"}`)
	p.parseLine(lineCompleted)
	res := p.result(0, nil)
	assert.False(t, res.IsError)
	assert.Nil(t, res.RateLimit)
}

func TestLimitOnStderrOnly(t *testing.T) {
	p := newParser()
	res := p.result(1, []string{"Error: You've hit your usage limit. try again in 30 minutes"})
	require.NotNil(t, res.RateLimit)
	assert.False(t, res.RateLimit.ResetAt.IsZero())
}
