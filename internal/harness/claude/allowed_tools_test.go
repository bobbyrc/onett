package claude

import (
	"slices"
	"testing"

	"github.com/bobbyrc/onett/internal/harness"
)

func TestBuildArgsAllowedTools(t *testing.T) {
	plain := buildArgs(harness.Config{}, harness.Spec{Prompt: "hi"})
	if slices.Contains(plain, "--allowedTools") {
		t.Fatalf("a run without AllowedTools got the flag: %v", plain)
	}

	spec := harness.Spec{Prompt: "hi", AllowedTools: []string{"Bash(onett:*)"}}
	args := buildArgs(harness.Config{Args: []string{"--allowedTools", "Bash(echo:*)"}}, spec)
	i := slices.Index(args, "Bash(onett:*)")
	if i < 1 || args[i-1] != "--allowedTools" {
		t.Fatalf("missing --allowedTools Bash(onett:*): %v", args)
	}
	// The owner's own rule stays too: the flags add up.
	if !slices.Contains(args, "Bash(echo:*)") {
		t.Fatalf("owner's allowed tool was dropped: %v", args)
	}
}
