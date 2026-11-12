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

// fakeFriction serves two friction reports and records the query of the last request.
func fakeFriction(t *testing.T) *url.Values {
	t.Helper()
	updated := time.Date(2026, 11, 12, 9, 30, 0, 0, time.Local)
	query := &url.Values{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/friction", func(w http.ResponseWriter, r *http.Request) {
		*query = r.URL.Query()
		writeJSON(w, http.StatusOK, []map[string]any{
			{"id": "fr-1", "project": "onett", "run_id": "run-7", "title": hostile + " go test needs network",
				"body": "The sandbox blocks proxy.golang.org.\n\x1b[2Jsecond line\n", "fingerprint": "fp-1", "count": 3,
				"issue_url": "https://github.com/o/r/issues/12", "issue_state": "open", "updated_at": updated},
			{"id": "fr-2", "project": "onett", "title": "slow CI", "fingerprint": "fp-2", "count": 1, "updated_at": updated},
		})
	})
	fakeAPI(t, mux)
	return query
}

func TestFrictionTable(t *testing.T) {
	query := fakeFriction(t)
	code, out, errOut := runCLI("friction", "--project", "onett", "--limit", "5")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "onett", query.Get("project"))
	assert.Equal(t, "5", query.Get("limit"))
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, []string{"ID", "PROJECT", "COUNT", "ISSUE", "UPDATED", "TITLE"}, strings.Fields(lines[0]))
	for _, want := range []string{"fr-1", "3", "https://github.com/o/r/issues/12 (open)", "2026-11-12 09:30", "title go test needs network"} {
		assert.Contains(t, lines[1], want)
	}
	assert.Contains(t, lines[2], "fr-2")
	assert.Contains(t, lines[2], " - ", "no issue filed yet")
	assert.True(t, strings.HasSuffix(lines[2], "slow CI"), lines[2])
	assert.NotContains(t, out, "\x1b")
	assert.NotContains(t, out, "pwned")
}

func TestFrictionEmpty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/friction", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
	fakeAPI(t, mux)
	code, out, errOut := runCLI("friction")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "no friction\n", out)
}

func TestFrictionJSON(t *testing.T) {
	fakeFriction(t)
	code, out, errOut := runCLI("friction", "--json")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, `"id": "fr-1"`)
	assert.Contains(t, out, `"id": "fr-2"`)
}

func TestFrictionShow(t *testing.T) {
	fakeFriction(t)
	code, out, errOut := runCLI("friction", "show", "fr-1")
	require.Equal(t, 0, code, errOut)
	for _, want := range []string{"id: fr-1\n", "project: onett\n", "run_id: run-7\n",
		"title: title go test needs network\n", "count: 3\n",
		"issue_url: https://github.com/o/r/issues/12\n", "issue_state: open\n", "fingerprint: fp-1\n",
		"updated_at: 2026-11-12 09:30\n"} {
		assert.Contains(t, out, want)
	}
	assert.True(t, strings.HasSuffix(out, "\n\nThe sandbox blocks proxy.golang.org.\nsecond line\n"), out)
	assert.NotContains(t, out, "\x1b")

	code, out, errOut = runCLI("friction", "show", "fr-2")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "issue_url: -\n")
	assert.True(t, strings.HasSuffix(out, "\n\n(no body)\n"), out)

	code, out, errOut = runCLI("friction", "show", "fr-1", "--json")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, `"id": "fr-1"`)
	assert.NotContains(t, out, "fr-2")

	code, _, errOut = runCLI("friction", "show", "nope")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "no friction")

	code, _, errOut = runCLI("friction", "show")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "usage: onett friction show ID")
}

func TestFrictionHelpDoesNotCallAPI(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
	})
	fakeAPI(t, mux)
	for _, args := range [][]string{{"friction", "-h"}, {"friction", "show", "-h"}} {
		code, out, errOut := runCLI(args...)
		assert.Equal(t, 0, code, errOut)
		assert.Contains(t, out, "usage: onett friction")
		assert.Contains(t, out, "-json")
	}
}
