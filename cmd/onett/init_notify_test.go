package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bobbyrc/onett/internal/config"
)

// runInit runs `onett init` with scripted stdin in a temp HOME and returns the written config,
// the output, and the home directory.
func runInit(t *testing.T, isTTY bool, stdin string, args ...string) (cfgText, out, home string, err error) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ONETT_CONFIG", "")
	cfg := filepath.Join(home, "onett.yaml")
	var o bytes.Buffer
	e := &env{out: &o, errOut: &o, in: strings.NewReader(stdin), isTTY: isTTY}
	err = cmdInit(e, append([]string{"--config", cfg}, args...))
	b, _ := os.ReadFile(cfg)
	return string(b), o.String(), home, err
}

func loads(t *testing.T, text string) *config.Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "onett.yaml")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("config does not load: %v\n%s", err, text)
	}
	return c
}

func TestInitFlagsWriteNotifierAndChannel(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "x")
	t.Setenv("DISCORD_BOT_TOKEN", "x")
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"ntfy", []string{"--notify", "ntfy", "--ntfy-topic", "t-123"}, []string{"type: ntfy", `topic: "t-123"`}},
		{"ntfy server", []string{"--notify", "ntfy", "--ntfy-topic", "t", "--ntfy-server", "https://ntfy.example.com"}, []string{`server: "https://ntfy.example.com"`}},
		{"telegram", []string{"--channel", "telegram", "--user-ids", "42,43"}, []string{"type: telegram", "TELEGRAM_BOT_TOKEN", "[42, 43]"}},
		{"discord", []string{"--channel", "discord", "--user-ids", "123456789012345678"}, []string{"type: discord", "DISCORD_BOT_TOKEN", `["123456789012345678"]`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, out, home, err := runInit(t, false, "", c.args...)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range c.want {
				if !strings.Contains(text, w) {
					t.Errorf("config lacks %q:\n%s", w, text)
				}
			}
			loads(t, text)
			if strings.Contains(c.name, "telegram") || c.name == "discord" {
				b, err := os.ReadFile(filepath.Join(home, ".config", "onett", "secrets.env"))
				if err != nil || !strings.HasSuffix(string(b), "_BOT_TOKEN=\n") {
					t.Errorf("secrets.env = %q, %v", b, err)
				}
				fi, _ := os.Stat(filepath.Join(home, ".config", "onett", "secrets.env"))
				if fi.Mode().Perm() != 0o600 {
					t.Errorf("secrets.env mode %v", fi.Mode().Perm())
				}
				if !strings.Contains(out, "_BOT_TOKEN=...") {
					t.Errorf("output does not name the key:\n%s", out)
				}
			}
		})
	}
}

func TestInitFlagValidation(t *testing.T) {
	for _, args := range [][]string{
		{"--notify", "ntfy"},
		{"--notify", "sms"},
		{"--ntfy-topic", "t"},
		{"--channel", "slack", "--user-ids", "1"},
		{"--channel", "telegram"},
		{"--channel", "telegram", "--user-ids", "abc"},
		{"--user-ids", "1"},
	} {
		if _, _, _, err := runInit(t, false, "", args...); err == nil {
			t.Errorf("%v: want an error", args)
		}
	}
}

func TestInitInteractiveChoices(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "x")
	t.Setenv("DISCORD_BOT_TOKEN", "x")
	cases := []struct {
		name, in string
		want     string
	}{
		{"telegram", "1\n42\n", "type: telegram"},
		{"discord", "2\n123456789012345678\n", "type: discord"},
		{"ntfy", "3\nmy-topic\n\n", `topic: "my-topic"`},
		{"ntfy random", "3\n\n\n", `topic: "onett-`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, out, _, err := runInit(t, true, c.in)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(text, c.want) {
				t.Errorf("config lacks %q:\n%s", c.want, text)
			}
			if !strings.Contains(out, "How should onett reach you") {
				t.Errorf("no prompt:\n%s", out)
			}
			loads(t, text)
		})
	}
	for _, in := range []string{"4\n", "\n", "", "9\n"} {
		text, out, _, err := runInit(t, true, in)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(text, "ntfy") || strings.Contains(text, "channels:") {
			t.Errorf("skip %q wrote a notifier:\n%s", in, text)
		}
		if !strings.Contains(out, "will not be able to tell you when a goal needs you; run `onett doctor`") {
			t.Errorf("skip %q: no warning:\n%s", in, out)
		}
	}
}

func TestInitNonTTYNeverPrompts(t *testing.T) {
	text, out, _, err := runInit(t, false, "1\n42\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "How should onett reach you") || strings.Contains(text, "telegram") {
		t.Errorf("prompted without a terminal:\n%s", out)
	}
	if !strings.Contains(out, "onett doctor") || !strings.Contains(out, "--channel telegram|discord") {
		t.Errorf("no advice:\n%s", out)
	}
}

func TestInitKeepsExistingSecretsAndPrintsNoSecret(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "onett")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const secret = "TELEGRAM_BOT_TOKEN=sekrit-value\n"
	if err := os.WriteFile(filepath.Join(dir, "secrets.env"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	var o bytes.Buffer
	cfg := filepath.Join(home, "onett.yaml")
	e := &env{out: &o, errOut: &o, in: strings.NewReader("")}
	if err := cmdInit(e, []string{"--config", cfg, "--channel", "telegram", "--user-ids", "7"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "secrets.env"))
	if string(b) != secret {
		t.Errorf("secrets.env changed: %q", b)
	}
	text, _ := os.ReadFile(cfg)
	if strings.Contains(string(text), "sekrit") || strings.Contains(o.String(), "sekrit") {
		t.Error("a secret value leaked")
	}
}
