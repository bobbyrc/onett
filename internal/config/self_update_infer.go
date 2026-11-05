package config

import (
	"os"
	"path/filepath"
	"strings"
)

// InferSelfUpdateCommand returns the loops.self_update command onett can safely work out for
// itself, or "" when it cannot. It answers only for the layout examples/dogfood/README.md sets
// up, and only when every part agrees, so it never runs the wrong binary or config:
//
//   - the running binary is <dir>/onett and the config being served is <dir>/onett.yaml;
//   - data_dir is <dir>/data (where update.sh leaves its result file);
//   - self_project's clone has examples/dogfood/update.sh;
//   - mise is on PATH (update.sh needs the pinned Go and Node).
//
// <dir> becomes the script's DOGFOOD. Nothing is inferred when self_update is switched off, when
// a command is already set, or when there is no self_project. lookPath is exec.LookPath.
func (c *Config) InferSelfUpdateCommand(configPath, exe string, lookPath func(string) (string, error)) string {
	p, ok := c.Projects[c.SelfProject]
	if c.SelfProject == "" || !ok {
		return ""
	}
	su := c.LoopsFor(c.SelfProject).SelfUpdate
	if (su.Enabled != nil && !*su.Enabled) || strings.TrimSpace(su.Command) != "" {
		return ""
	}
	if configPath == "" || exe == "" || !filepath.IsAbs(configPath) || !filepath.IsAbs(exe) {
		return ""
	}
	dir := filepath.Dir(exe)
	if filepath.Base(exe) != "onett" ||
		filepath.Clean(configPath) != filepath.Join(dir, "onett.yaml") ||
		filepath.Clean(c.Server.DataDir) != filepath.Join(dir, "data") {
		return ""
	}
	script := filepath.Join(p.Path, "examples", "dogfood", "update.sh")
	if info, err := os.Stat(script); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	mise, err := lookPath("mise")
	if err != nil || mise == "" {
		return ""
	}
	return "DOGFOOD=" + shellQuote(dir) + " " + shellQuote(mise) + " exec -- " + shellQuote(script)
}

// ApplySelfUpdateInference fills in the self_update command when it can be inferred (see
// InferSelfUpdateCommand) and returns it, or "" when nothing changed.
func (c *Config) ApplySelfUpdateInference(configPath, exe string, lookPath func(string) (string, error)) string {
	cmd := c.InferSelfUpdateCommand(configPath, exe, lookPath)
	if cmd != "" {
		c.Loops.SelfUpdate.Command = cmd
	}
	return cmd
}

// shellQuote single-quotes s for sh -c.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
