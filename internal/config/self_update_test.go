package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const suDoc = `projects:
  app:
    repo: o/app
    path: /tmp/app
%s
self_project: app
`

func TestSelfUpdateDefaultsOffAndSelfOnly(t *testing.T) {
	cfg, err := Parse([]byte(`projects:
  app:
    repo: o/app
    path: /tmp/app
self_project: app
`), "")
	require.NoError(t, err)
	l := cfg.LoopsFor("app")
	assert.False(t, l.SelfUpdateOn("app", cfg.SelfProject), "off unless enabled")
	assert.Equal(t, 5*time.Minute, l.SelfUpdate.Every.Duration)
	assert.Equal(t, 10*time.Minute, l.SelfUpdate.MinHealthy.Duration)
}

func TestSelfUpdateEnabledNeedsCommand(t *testing.T) {
	_, err := Parse([]byte(`projects:
  app:
    repo: o/app
    path: /tmp/app
    loops:
      self_update:
        enabled: true
self_project: app
`), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loops.self_update.command")

	cfg, err := Parse([]byte(`projects:
  app:
    repo: o/app
    path: /tmp/app
    loops:
      self_update:
        enabled: true
        command: ./update.sh
        min_healthy: 30m
  other:
    repo: o/other
    path: /tmp/other
self_project: app
`), "")
	require.NoError(t, err)
	assert.True(t, cfg.LoopsFor("app").SelfUpdateOn("app", "app"))
	assert.Equal(t, 30*time.Minute, cfg.LoopsFor("app").SelfUpdate.MinHealthy.Duration)
	assert.False(t, cfg.LoopsFor("other").SelfUpdateOn("other", "app"), "only the self project")
}
