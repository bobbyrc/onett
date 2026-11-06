package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A trimmed copy of the token_count event Codex writes to its rollout file.
const rolloutTokenCount = `{"timestamp":"2026-11-06T16:49:57.218Z","ordinal":18,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":37777,"cached_input_tokens":13184,"output_tokens":6},"last_token_usage":{"input_tokens":37777,"output_tokens":6},"model_context_window":475000},"rate_limits":null}}`

func TestRolloutWindowReadsTokenCount(t *testing.T) {
	in := `{"type":"session_meta","payload":{"model_context_window":1}}` + "\n" + rolloutTokenCount + "\n"
	if got := rolloutWindow(strings.NewReader(in)); got != 475000 {
		t.Fatalf("window = %d, want 475000", got)
	}
	if got := rolloutWindow(strings.NewReader(`{"type":"event_msg","payload":{"type":"token_count","info":null}}`)); got != 0 {
		t.Fatalf("window = %d, want 0", got)
	}
}

func TestSessionContextWindowFindsRollout(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "11", "06")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "rollout-2026-11-06T11-49-35-thread-1.jsonl")
	if err := os.WriteFile(file, []byte(rolloutTokenCount+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := sessionContextWindow(home, "thread-1"); got != 475000 {
		t.Fatalf("window = %d, want 475000", got)
	}
	if got := sessionContextWindow(home, "other"); got != 0 {
		t.Fatalf("unknown thread window = %d, want 0", got)
	}
	if got := sessionContextWindow(home, "../x"); got != 0 {
		t.Fatalf("path-like id window = %d, want 0", got)
	}
}
