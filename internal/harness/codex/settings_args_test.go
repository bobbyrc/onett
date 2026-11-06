package codex

import (
	"slices"
	"testing"

	"github.com/bobbyrc/onett/internal/harness"
)

func TestBuildArgsCarriesConversationSettings(t *testing.T) {
	args := buildArgs(harness.Config{}, harness.Spec{
		Prompt: "hi", Model: "gpt-x", Effort: "high", PermissionMode: "read-only",
	})
	for _, want := range [][]string{
		{"--model", "gpt-x"}, {"-c", `model_reasoning_effort="high"`}, {"--sandbox", "read-only"},
	} {
		i := slices.Index(args, want[0])
		if i < 0 || args[i+1] != want[1] {
			t.Errorf("missing %v in %v", want, args)
		}
	}
	if slices.Contains(args, "--approve-for-me") {
		t.Errorf("a chosen sandbox must replace the default approval flag: %v", args)
	}
}

func TestBuildArgsBypassAndResume(t *testing.T) {
	args := buildArgs(harness.Config{}, harness.Spec{Prompt: "hi", PermissionMode: permBypass})
	if !slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Errorf("bypass flag missing: %v", args)
	}
	// `codex exec resume` has no sandbox flags, so the mode becomes config overrides.
	args = buildArgs(harness.Config{}, harness.Spec{Prompt: "hi", ResumeSessionID: "s1", PermissionMode: "read-only"})
	if slices.Contains(args, "--sandbox") || !slices.Contains(args, `sandbox_mode="read-only"`) {
		t.Errorf("resume args = %v", args)
	}
}

func TestChoicesListCodexSettings(t *testing.T) {
	c := harness.ChoicesFor(harnessType)
	var modes []string
	for _, m := range c.PermissionModes {
		modes = append(modes, m.Value)
	}
	want := []string{"approve-for-me", "read-only", "workspace-write", "danger-full-access", permBypass}
	if !slices.Equal(modes, want) {
		t.Errorf("permission modes = %v", modes)
	}
	if len(c.Efforts) == 0 {
		t.Error("no efforts")
	}
}
