package engine

import (
	"strings"
	"testing"
)

func TestCIFailureContextFencesAndLabelsLog(t *testing.T) {
	got := ciFailureContext("=== Go / vet ===\n```\nmain.go:3: bad\n", nil)
	for _, want := range []string{evidenceNote, "=== Go / vet ===", "main.go:3: bad", "````\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("context lacks %q: %q", want, got)
		}
	}
}

func TestCIFailureContextRedactsSecrets(t *testing.T) {
	got := ciFailureContext("token=hunter2hunter2 leaked", []string{"API_TOKEN=hunter2hunter2"})
	if strings.Contains(got, "hunter2hunter2") {
		t.Fatalf("secret leaked: %q", got)
	}
}

func TestCIFailureContextSaysWhenLogIsEmpty(t *testing.T) {
	got := ciFailureContext("  \n", nil)
	if !strings.Contains(got, "No failed-step log was available") {
		t.Fatalf("empty log not explained: %q", got)
	}
}
