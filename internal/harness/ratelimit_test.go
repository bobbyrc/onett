package harness

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestResetFromText(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"try again in 2 hours 5 minutes": now.Add(2*time.Hour + 5*time.Minute),
		"Try again in 37m":               now.Add(37 * time.Minute),
		"resets 3pm":                     time.Date(2026, 11, 4, 15, 0, 0, 0, time.UTC),
		"resets 9:30 AM":                 time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC),
		"limit reached|1893456000":       time.Unix(1893456000, 0),
		"limit reached|1000000000":       {}, // in the past
		"no hint here":                   {},
		"try again at 25:00":             {},
	}
	for text, want := range cases {
		got := ResetFromText(text, now)
		assert.True(t, got.Equal(want), "%q: got %v want %v", text, got, want)
	}
}

func TestLooksRateLimited(t *testing.T) {
	assert.True(t, LooksRateLimited("You've hit your usage limit"))
	assert.True(t, LooksRateLimited("HTTP 429 Too Many Requests"))
	assert.False(t, LooksRateLimited("Reconnecting... 1/5"))
	assert.False(t, LooksRateLimited("file 4290 not found"))
}
