package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSignals serves two signals and records the query of the last request.
func fakeSignals(t *testing.T) *url.Values {
	t.Helper()
	seen := time.Date(2026, 11, 12, 9, 30, 0, 0, time.Local)
	query := &url.Values{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/signals", func(w http.ResponseWriter, r *http.Request) {
		*query = r.URL.Query()
		writeJSON(w, http.StatusOK, []map[string]any{
			{"id": "sig-1", "project": "onett", "source": "sentry", "fingerprint": "fp-1",
				"title": hostile + " nil pointer\nin handler", "level": "error", "count": 42,
				"url": "https://sentry.example/1", "culprit": "api.getTask", "release": "abc123",
				"task_id": "task-9", "first_seen": seen, "last_seen": seen},
			{"id": "sig-2", "project": "onett", "source": "sentry", "title": "slow query",
				"level": "warning", "count": 1, "last_seen": seen},
		})
	})
	fakeAPI(t, mux)
	return query
}

func TestSignalsTable(t *testing.T) {
	query := fakeSignals(t)
	code, out, errOut := runCLI("signals", "--project", "onett", "--limit", "5")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "onett", query.Get("project"))
	assert.Equal(t, "5", query.Get("limit"))
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, []string{"ID", "PROJECT", "SOURCE", "LEVEL", "COUNT", "TASK", "LAST", "SEEN", "TITLE"},
		strings.Fields(lines[0]))
	for _, want := range []string{"sig-1", "sentry", "error", "42", "task-9", "2026-11-12 09:30", "title nil pointer in handler"} {
		assert.Contains(t, lines[1], want)
	}
	assert.Contains(t, lines[2], "sig-2")
	assert.Contains(t, lines[2], " - ", "no task yet")
	assert.NotContains(t, out, "\x1b")
	assert.NotContains(t, out, "pwned")
}

func TestSignalsEmpty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/signals", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
	fakeAPI(t, mux)
	code, out, errOut := runCLI("signals")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "no signals\n", out)
}

func TestSignalsJSON(t *testing.T) {
	fakeSignals(t)
	code, out, errOut := runCLI("signals", "--json")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, `"id": "sig-1"`)
	assert.Contains(t, out, `"id": "sig-2"`)
}

func TestSignalsShow(t *testing.T) {
	fakeSignals(t)
	code, out, errOut := runCLI("signals", "show", "sig-1")
	require.Equal(t, 0, code, errOut)
	for _, want := range []string{"id: sig-1\n", "project: onett\n", "source: sentry\n", "level: error\n",
		"count: 42\n", "title: title nil pointer in handler\n", "culprit: api.getTask\n", "release: abc123\n",
		"url: https://sentry.example/1\n", "fingerprint: fp-1\n", "task_id: task-9\n",
		"last_seen: 2026-11-12 09:30\n"} {
		assert.Contains(t, out, want)
	}
	assert.NotContains(t, out, "\x1b")

	code, out, errOut = runCLI("signals", "show", "sig-2")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "task_id: -\n")
	assert.Contains(t, out, "url: -\n")

	code, out, errOut = runCLI("signals", "show", "sig-1", "--json")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, `"id": "sig-1"`)
	assert.NotContains(t, out, "sig-2")

	code, _, errOut = runCLI("signals", "show", "nope")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "no signal")

	code, _, errOut = runCLI("signals", "show")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "usage: onett signals show ID")
}

func TestSignalsHelpDoesNotCallAPI(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
	})
	fakeAPI(t, mux)
	for _, args := range [][]string{{"signals", "-h"}, {"signals", "show", "-h"}} {
		code, out, errOut := runCLI(args...)
		assert.Equal(t, 0, code, errOut)
		assert.Contains(t, out, "usage: onett signals")
		assert.Contains(t, out, "-json")
	}
}
