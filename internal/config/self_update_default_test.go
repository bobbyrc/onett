package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func suCfg(t *testing.T, loops string) *Config {
	t.Helper()
	doc := `projects:
  app:
    repo: o/app
    path: /tmp/app
` + loops + `  other:
    repo: o/other
    path: /tmp/other
self_project: app
`
	cfg, err := Parse([]byte(doc), "")
	require.NoError(t, err)
	return cfg
}

func TestSelfUpdateEnabledTriState(t *testing.T) {
	const on = "    loops:\n      self_update:\n        command: ./u.sh\n"
	cmdOnly := suCfg(t, on)
	assert.True(t, cmdOnly.LoopsFor("app").SelfUpdateOn("app", "app"), "unset enabled with a command is on")
	assert.False(t, cmdOnly.LoopsFor("other").SelfUpdateOn("other", "app"), "other projects unaffected")

	off := suCfg(t, "    loops:\n      self_update:\n        enabled: false\n        command: ./u.sh\n")
	assert.False(t, off.LoopsFor("app").SelfUpdateOn("app", "app"), "explicit false wins")

	explicit := suCfg(t, "    loops:\n      self_update:\n        enabled: true\n        command: ./u.sh\n")
	assert.True(t, explicit.LoopsFor("app").SelfUpdateOn("app", "app"))

	none := suCfg(t, "")
	assert.False(t, none.LoopsFor("app").SelfUpdateOn("app", "app"), "no command means off")
	assert.False(t, cmdOnly.LoopsFor("app").SelfUpdateOn("app", ""), "no self_project means off")
}

// dogfoodLayout builds <dir>/{onett,onett.yaml,data} and a clone with update.sh.
func dogfoodLayout(t *testing.T) (cfg *Config, cfgPath, exe string) {
	t.Helper()
	dir := t.TempDir()
	clone := filepath.Join(t.TempDir(), "clone")
	require.NoError(t, os.MkdirAll(filepath.Join(clone, "examples", "dogfood"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(clone, "examples", "dogfood", "update.sh"), []byte("#!/bin/sh\n"), 0o755))
	cfgPath = filepath.Join(dir, "onett.yaml")
	doc := "server:\n  data_dir: " + filepath.Join(dir, "data") + "\nprojects:\n  onett:\n    repo: o/onett\n    path: " + clone + "\nself_project: onett\n"
	var err error
	cfg, err = Parse([]byte(doc), cfgPath)
	require.NoError(t, err)
	return cfg, cfgPath, filepath.Join(dir, "onett")
}

func lookMise(name string) (string, error) {
	if name == "mise" {
		return "/opt/bin/mise", nil
	}
	return "", errors.New("not found")
}

func TestInferSelfUpdateCommand(t *testing.T) {
	cfg, cfgPath, exe := dogfoodLayout(t)
	got := cfg.InferSelfUpdateCommand(cfgPath, exe, lookMise)
	dir := filepath.Dir(exe)
	assert.Equal(t, "DOGFOOD='"+dir+"' '/opt/bin/mise' exec -- '"+filepath.Join(cfg.Projects["onett"].Path, "examples/dogfood/update.sh")+"'", got)

	assert.Equal(t, got, cfg.ApplySelfUpdateInference(cfgPath, exe, lookMise))
	assert.True(t, cfg.LoopsFor("onett").SelfUpdateOn("onett", "onett"))
}

func TestInferSelfUpdateCommandRefuses(t *testing.T) {
	t.Run("config elsewhere", func(t *testing.T) {
		cfg, cfgPath, exe := dogfoodLayout(t)
		assert.Empty(t, cfg.InferSelfUpdateCommand(filepath.Join(t.TempDir(), "onett.yaml"), exe, lookMise))
		_ = cfgPath
	})
	t.Run("binary not named onett", func(t *testing.T) {
		cfg, cfgPath, exe := dogfoodLayout(t)
		assert.Empty(t, cfg.InferSelfUpdateCommand(cfgPath, exe+"-x", lookMise))
	})
	t.Run("data dir elsewhere", func(t *testing.T) {
		cfg, cfgPath, exe := dogfoodLayout(t)
		cfg.Server.DataDir = t.TempDir()
		assert.Empty(t, cfg.InferSelfUpdateCommand(cfgPath, exe, lookMise))
	})
	t.Run("no update.sh", func(t *testing.T) {
		cfg, cfgPath, exe := dogfoodLayout(t)
		p := cfg.Projects["onett"]
		p.Path = t.TempDir()
		cfg.Projects["onett"] = p
		assert.Empty(t, cfg.InferSelfUpdateCommand(cfgPath, exe, lookMise))
	})
	t.Run("no mise", func(t *testing.T) {
		cfg, cfgPath, exe := dogfoodLayout(t)
		assert.Empty(t, cfg.InferSelfUpdateCommand(cfgPath, exe, func(string) (string, error) { return "", errors.New("no") }))
	})
	t.Run("disabled", func(t *testing.T) {
		cfg, cfgPath, exe := dogfoodLayout(t)
		off := false
		cfg.Loops.SelfUpdate.Enabled = &off
		assert.Empty(t, cfg.InferSelfUpdateCommand(cfgPath, exe, lookMise))
	})
	t.Run("command already set", func(t *testing.T) {
		cfg, cfgPath, exe := dogfoodLayout(t)
		cfg.Loops.SelfUpdate.Command = "mine"
		assert.Empty(t, cfg.InferSelfUpdateCommand(cfgPath, exe, lookMise))
		assert.Equal(t, "mine", cfg.Loops.SelfUpdate.Command)
	})
	t.Run("no self project", func(t *testing.T) {
		cfg, cfgPath, exe := dogfoodLayout(t)
		cfg.SelfProject = ""
		assert.Empty(t, cfg.InferSelfUpdateCommand(cfgPath, exe, lookMise))
	})
}
