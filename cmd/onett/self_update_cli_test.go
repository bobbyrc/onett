package main

import (
	"bytes"
	"testing"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitialConfigExplainsSelfUpdateForSelfRepo(t *testing.T) {
	got := initialConfig("onett", selfRepo, "")
	assert.Contains(t, got, "Self-update is on by default")
	assert.Contains(t, got, "enabled: false")
	assert.Contains(t, got, "update.sh")
	_, err := config.Parse([]byte(got), "")
	require.NoError(t, err, "the comments must leave a loadable file")

	assert.NotContains(t, initialConfig("app", "o/app", ""), "Self-update")
}

func TestDoctorSelfUpdateLine(t *testing.T) {
	parse := func(extra string) *config.Config {
		cfg, err := config.Parse([]byte("projects:\n  app:\n    repo: o/app\n    path: /tmp/app\n"+extra+"self_project: app\n"), "")
		require.NoError(t, err)
		return cfg
	}
	run := func(cfg *config.Config) string {
		var buf bytes.Buffer
		(&doctor{out: &buf}).checkSelfUpdate(cfg)
		return buf.String()
	}
	assert.Contains(t, run(parse("")), "warn  self-update is off: no command — fix: set loops.self_update.command")
	assert.Contains(t, run(parse("    loops:\n      self_update:\n        command: ./u.sh\n")), "ok    self-update is on for app")
	assert.Empty(t, run(parse("    loops:\n      self_update:\n        enabled: false\n")), "off on purpose is silent")
}
