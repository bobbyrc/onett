package claude

import (
	"slices"
	"testing"

	"github.com/bobbyrc/onett/internal/harness"
)

// argAfter returns the value following flag, or "" when the flag is missing.
func argAfter(args []string, flag string) string {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

func TestBuildArgsCarriesConversationSettings(t *testing.T) {
	args := buildArgs(harness.Config{}, harness.Spec{
		Prompt: "hi", Model: "opus", Effort: "high", PermissionMode: "plan",
	})
	for flag, want := range map[string]string{"--model": "opus", "--effort": "high", "--permission-mode": "plan"} {
		if got := argAfter(args, flag); got != want {
			t.Errorf("%s = %q, want %q in %v", flag, got, want, args)
		}
	}
	if n := countOf(args, "--permission-mode"); n != 1 {
		t.Errorf("--permission-mode appears %d times: %v", n, args)
	}
}

func TestBuildArgsWithoutSettingsKeepsDefaults(t *testing.T) {
	args := buildArgs(harness.Config{}, harness.Spec{Prompt: "hi"})
	if slices.Contains(args, "--effort") || slices.Contains(args, "--model") {
		t.Errorf("unset settings added flags: %v", args)
	}
	if got := argAfter(args, "--permission-mode"); got != defaultPermissionMode {
		t.Errorf("permission mode = %q, want the default %q", got, defaultPermissionMode)
	}
}

// A mode chosen for the conversation is last, so it wins over one in the agent profile's args.
func TestBuildArgsSettingsModeWinsOverProfile(t *testing.T) {
	args := buildArgs(harness.Config{}, harness.Spec{
		Prompt: "hi", PermissionMode: "plan", Args: []string{"--permission-mode", "acceptEdits"},
	})
	if i := slices.Index(args, "plan"); i < 0 || i < slices.Index(args, "acceptEdits") {
		t.Errorf("the conversation's mode must come after the profile's: %v", args)
	}
}

func TestSettingsChangeChangesConversationFingerprint(t *testing.T) {
	r := &runner{}
	base := r.fingerprint(harness.Spec{Model: "opus"})
	if r.fingerprint(harness.Spec{Model: "opus"}) != base {
		t.Fatal("same settings must give the same fingerprint")
	}
	for _, s := range []harness.Spec{{Model: "sonnet"}, {Model: "opus", Effort: "max"}, {Model: "opus", PermissionMode: "plan"}} {
		if r.fingerprint(s) == base {
			t.Errorf("%+v kept the live process: its settings are fixed when it starts", s)
		}
	}
}

func TestChoicesListClaudeSettings(t *testing.T) {
	c := harness.ChoicesFor(harnessType)
	values := func(list []harness.Choice) []string {
		var out []string
		for _, x := range list {
			out = append(out, x.Value)
		}
		return out
	}
	if got := values(c.Models); !slices.Equal(got, []string{"opus", "sonnet", "haiku"}) {
		t.Errorf("models = %v", got)
	}
	if got := values(c.Efforts); !slices.Equal(got, []string{"low", "medium", "high", "xhigh", "max"}) {
		t.Errorf("efforts = %v", got)
	}
	if got := values(c.PermissionModes); !slices.Equal(got,
		[]string{"auto", "acceptEdits", "plan", "dontAsk", "manual", "bypassPermissions"}) {
		t.Errorf("permission modes = %v", got)
	}
	for _, m := range c.PermissionModes {
		if m.Dangerous != (m.Value == "bypassPermissions") {
			t.Errorf("%s dangerous = %v", m.Value, m.Dangerous)
		}
	}
}
