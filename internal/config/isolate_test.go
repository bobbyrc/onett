package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHarnessIsolateIsOffByDefaultAndCanBeEnabled(t *testing.T) {
	cfg, err := Parse(nil, "")
	require.NoError(t, err)
	assert.False(t, cfg.Harness["claude"].IsolateOn())

	cfg, err = Parse([]byte("harnesses:\n  claude:\n    type: claude-code\n    isolate: true\n"), "")
	require.NoError(t, err)
	assert.True(t, cfg.Harness["claude"].IsolateOn())
}

func TestChatSessionIdleTimeoutDefaultsTo30mAndNegativeTurnsItOff(t *testing.T) {
	cfg, err := Parse(nil, "")
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, cfg.Chat.SessionIdleTimeout.Duration)

	cfg, err = Parse([]byte("chat:\n  session_idle_timeout: 5m\n"), "")
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, cfg.Chat.SessionIdleTimeout.Duration)

	cfg, err = Parse([]byte("chat:\n  session_idle_timeout: -1s\n"), "")
	require.NoError(t, err)
	assert.Negative(t, cfg.Chat.SessionIdleTimeout.Duration)
}
