package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetiredMaxDeferStillLoadsWithWarning(t *testing.T) {
	cfg, err := Parse([]byte(`
loops:
  self_update:
    min_healthy: 15m
    max_defer: 2h
projects:
  app:
    repo: o/app
    loops:
      self_update:
        max_defer: 1h
`), "")
	require.NoError(t, err)
	assert.Equal(t, "15m0s", cfg.Loops.SelfUpdate.MinHealthy.String(), "other keys are kept")
	require.Len(t, cfg.Warnings, 2)
	assert.Contains(t, cfg.Warnings[0], "loops.self_update.max_defer is retired")
	assert.Contains(t, cfg.Warnings[1], "projects.app.loops.self_update.max_defer is retired")
}

func TestUnknownKeysStillRefused(t *testing.T) {
	_, err := Parse([]byte("loops:\n  self_update:\n    max_deferr: 2h\n"), "")
	require.Error(t, err)
}
