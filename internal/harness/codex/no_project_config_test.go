package codex

import (
	"slices"
	"testing"

	"github.com/bobbyrc/onett/internal/harness"
)

func TestBuildArgsNoProjectConfigLeavesOutProjectDoc(t *testing.T) {
	specs := map[string]harness.Spec{
		"new": {Prompt: "hi", NoProjectConfig: true, Args: []string{"-c", "project_doc_max_bytes=4096"}},
		"resume": {
			Prompt: "hi", NoProjectConfig: true, ResumeSessionID: "s1",
			Args: []string{"-c", "project_doc_max_bytes=4096"},
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			args := buildArgs(harness.Config{}, spec)
			last := -1
			for i := 0; i+1 < len(args); i++ {
				if args[i] == "-c" && args[i+1] == "project_doc_max_bytes=0" {
					last = i
				}
			}
			if last == -1 {
				t.Fatalf("no project_doc_max_bytes=0 override in %v", args)
			}
			extra := slices.Index(args, "project_doc_max_bytes=4096")
			if extra == -1 || last <= extra {
				t.Errorf("override at %d must come after the extra budget at %d in %v", last, extra, args)
			}
			if got := args[len(args)-1]; got != "-" {
				t.Errorf("last arg = %q, want \"-\" (prompt from stdin)", got)
			}
		})
	}
}

func TestBuildArgsWithoutNoProjectConfigHasNoProjectDocOverride(t *testing.T) {
	args := buildArgs(harness.Config{}, harness.Spec{Prompt: "hi"})
	if slices.Contains(args, "project_doc_max_bytes=0") {
		t.Errorf("unexpected project_doc_max_bytes=0 in %v", args)
	}
}
