package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bobbyrc/onett/internal/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusWarnsWhenOwnerUnreachable(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeStatus(&buf, statusSnapshot{Stats: client.Stats{OwnerUnreachable: true}}))
	out := buf.String()
	assert.Contains(t, out, "Warning: Only the log notifier is configured")
	assert.Contains(t, out, "docs/channels.md")
	assert.NotContains(t, out, "you have not been notified")
	assert.NotContains(t, out, "  nothing\n\nRunning", "the warning counts as something that needs the owner")
}

func TestStatusNamesBlockedGoalWhenOwnerUnreachable(t *testing.T) {
	snap := statusSnapshot{Stats: client.Stats{
		OwnerUnreachable: true,
		BlockedGoals:     []client.BlockedGoal{{ID: "g1", Reason: "no progress"}},
	}}
	var buf bytes.Buffer
	require.NoError(t, writeStatus(&buf, snap))
	assert.Contains(t, buf.String(), "Goal g1 is blocked and you have not been notified: no progress")
}

func TestStatusQuietWhenOwnerReachable(t *testing.T) {
	snap := statusSnapshot{Stats: client.Stats{BlockedGoals: []client.BlockedGoal{{ID: "g1", Reason: "x"}}}}
	var buf bytes.Buffer
	require.NoError(t, writeStatus(&buf, snap))
	assert.False(t, strings.Contains(buf.String(), "Warning"), buf.String())
	assert.NotContains(t, buf.String(), "g1")
}

func TestDoctorWarnsWhenOnlyLogNotifier(t *testing.T) {
	healthyDaemon(t)
	f := newDoctorFixture(t, "")
	toolRunner(t, nil)
	code, out, errOut := runCLI("doctor", "--config", f.cfg)
	require.Equal(t, 0, code, out+errOut)
	assertLine(t, out, "warn", "only the log notifier is configured")
	assertLine(t, out, "warn", "fix: set up a channel or notifier")
}

func TestDoctorOKWithNtfyNotifier(t *testing.T) {
	healthyDaemon(t)
	f := newDoctorFixture(t, "notify:\n  - type: ntfy\n    options: {topic: x}\n")
	toolRunner(t, nil)
	code, out, errOut := runCLI("doctor", "--config", f.cfg)
	require.Equal(t, 0, code, out+errOut)
	assertLine(t, out, "ok", "owner reachable")
	assert.NotContains(t, out, "only the log notifier")
}
