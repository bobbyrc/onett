package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTriggerListPrintsATable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/triggers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{
			map[string]any{"id": "tr1", "name": "Morning", "kind": "schedule", "project": "onett",
				"enabled": true, "cron": "0 5 * * *", "time_zone": "America/Chicago",
				"next_run_at": "2026-11-13T11:00:00Z",
				"last_firing": map[string]any{"id": "f1", "outcome": "started", "at": "2026-11-12T11:00:00Z"}},
			map[string]any{"id": "tr2", "name": "Bugs", "kind": "github_issue", "project": "onett",
				"enabled": false, "label": "bug"},
		})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("trigger", "list")
	require.Equal(t, 0, code, errOut)
	for _, want := range []string{"ID", "NAME", "KIND", "PROJECT", "ENABLED", "WHEN", "NEXT RUN", "LAST FIRING",
		"0 5 * * * (America/Chicago)", "new issue labelled bug", "yes", "no", "started"} {
		assert.Contains(t, out, want)
	}
}

func TestTriggerListEmpty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/triggers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("trigger", "list")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "no triggers\n", out)
}

func TestTriggerHistoryPrintsFirings(t *testing.T) {
	var query string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/triggers/tr1/firings", func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		writeJSON(w, http.StatusOK, []any{
			map[string]any{"id": "f1", "cause": "schedule", "outcome": "started", "at": "2026-11-12T11:00:00Z",
				"scheduled_for": "2026-11-12T11:00:00Z", "conversation_id": "conv-1"},
			map[string]any{"id": "f2", "cause": "github_issue", "outcome": "skipped", "issue_number": 12,
				"reason": "the factory is paused", "at": "2026-11-12T10:00:00Z"},
			map[string]any{"id": "f3", "cause": "manual", "by": "owner:alice", "outcome": "failed",
				"reason": "boom", "at": "2026-11-12T09:00:00Z"},
		})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("trigger", "history", "tr1")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "limit=20", query)
	for _, want := range []string{"AT", "CAUSE", "OUTCOME", "REASON", "CONVERSATION",
		"issue #12", "manual by owner:alice", "skipped", "the factory is paused", "failed", "boom", "conv-1"} {
		assert.Contains(t, out, want)
	}

	code, _, errOut = runCLI("trigger", "history", "tr1", "--limit", "5")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "limit=5", query)
}

func TestTriggerHistoryNeedsAnID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
	})
	fakeAPI(t, mux)

	code, _, errOut := runCLI("trigger", "history")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "usage: onett trigger history ID")
}

func TestTriggerHasNoMutatingVerbs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
	})
	fakeAPI(t, mux)

	for _, verb := range []string{"create", "update", "delete", "enable", "disable", "run"} {
		code, out, errOut := runCLI("trigger", verb, "tr1")
		assert.Equal(t, 2, code, verb)
		assert.Empty(t, out, verb)
		assert.Contains(t, errOut, "unknown trigger command", verb)
	}
}

func TestTriggerHelp(t *testing.T) {
	code, out, errOut := runCLI("trigger", "-h")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "usage: onett trigger")

	code, out, errOut = runCLI("trigger", "list", "-h")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "usage: onett trigger list [--json]")
	assert.Contains(t, out, "the CLI only reads them")

	code, out, errOut = runCLI("trigger", "history", "-h")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "usage: onett trigger history ID [--limit N]")
}
