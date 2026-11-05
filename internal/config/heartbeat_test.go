package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeartbeatDefaults(t *testing.T) {
	cfg, err := Parse([]byte("projects: {}\n"), "")
	require.NoError(t, err)
	hb := cfg.Loops.Heartbeat
	assert.True(t, hb.On())
	assert.Equal(t, 15*time.Minute, hb.Every.Duration)
	assert.Equal(t, 4*time.Hour, hb.StaleQueued.Duration)
	assert.Empty(t, hb.QuietHours)
}

func TestHeartbeatSettingsAndProjectOverride(t *testing.T) {
	cfg, err := Parse([]byte(`
loops:
  heartbeat:
    every: 5m
    quiet_hours: "22:00-07:00"
    stale_queued: 2h
projects:
  app:
    repo: owner/app
    path: /tmp/app
    loops:
      heartbeat:
        enabled: false
        quiet_hours: "01:00-02:00"
`), "")
	require.NoError(t, err)
	hb := cfg.Loops.Heartbeat
	assert.Equal(t, 5*time.Minute, hb.Every.Duration)
	assert.Equal(t, "22:00-07:00", hb.QuietHours)
	assert.Equal(t, 2*time.Hour, hb.StaleQueued.Duration)

	app := cfg.LoopsFor("app").Heartbeat
	assert.False(t, app.On())
	assert.Equal(t, "01:00-02:00", app.QuietHours)
	assert.Equal(t, 5*time.Minute, app.Every.Duration, "unset fields keep the global value")
	assert.Equal(t, 2*time.Hour, app.StaleQueued.Duration)
}

func TestHeartbeatValidation(t *testing.T) {
	for _, tc := range []struct{ name, loops, want string }{
		{"bad format", `quiet_hours: "late"`, "loops.heartbeat.quiet_hours"},
		{"bad hour", `quiet_hours: "25:00-07:00"`, "loops.heartbeat.quiet_hours"},
		{"empty window", `quiet_hours: "08:00-08:00"`, "loops.heartbeat.quiet_hours"},
		{"negative stale", `stale_queued: -1h`, "loops.heartbeat.stale_queued"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte("loops:\n  heartbeat:\n    "+tc.loops+"\nprojects: {}\n"), "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
	_, err := Parse([]byte(`
projects:
  app:
    repo: owner/app
    path: /tmp/app
    loops:
      heartbeat:
        quiet_hours: "nope"
`), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "projects.app.loops.heartbeat.quiet_hours")
}

func TestQuietHoursContains(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 1, 2, h, m, 0, 0, time.UTC) }
	for _, tc := range []struct {
		window string
		t      time.Time
		want   bool
	}{
		{"", at(3, 0), false},
		{"09:00-17:00", at(9, 0), true},
		{"09:00-17:00", at(16, 59), true},
		{"09:00-17:00", at(17, 0), false},
		{"09:00-17:00", at(23, 0), false},
		{"22:00-07:00", at(22, 0), true},
		{"22:00-07:00", at(23, 59), true},
		{"22:00-07:00", at(0, 0), true},
		{"22:00-07:00", at(6, 59), true},
		{"22:00-07:00", at(7, 0), false},
		{"22:00-07:00", at(12, 0), false},
		{" 22:00 - 07:00 ", at(1, 0), true},
	} {
		q, err := ParseQuietHours(tc.window)
		require.NoError(t, err, tc.window)
		assert.Equal(t, tc.want, q.Contains(tc.t), "%q at %s", tc.window, tc.t.Format("15:04"))
	}
}
