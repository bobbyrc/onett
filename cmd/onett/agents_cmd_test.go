package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentsHidesRemovedUnlessAll(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/agents", func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.URL.Query().Get("project"), "the CLI asks for the global view")
		writeJSON(w, http.StatusOK, []map[string]any{
			{"name": "code-reviewer", "title": "Code Reviewer", "origin": "builtin", "disabled": false, "harness": "claude", "model": "sonnet", "kinds": []string{"review"}, "description": "reviews"},
			{"name": "platform-engineer", "origin": "builtin", "disabled": true, "harness": "codex", "kinds": []string{}, "description": "fixes friction"},
		})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("agents")
	require.Equal(t, 0, code, errOut)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 2)
	assert.Regexp(t, `^ROLE\s+TITLE\s+ORIGIN\s+HARNESS\s+MODEL\s+KINDS\s+DESCRIPTION$`, lines[0])
	assert.Regexp(t, `^code-reviewer\s+Code Reviewer\s+builtin\s+claude\s+sonnet\s+review\s+reviews$`, lines[1])
	assert.NotContains(t, out, "platform-engineer")

	code, out, errOut = runCLI("agents", "--json")
	require.Equal(t, 0, code, errOut)
	assert.NotContains(t, out, "improver")
	assert.Contains(t, out, `"name": "code-reviewer"`)

	code, out, errOut = runCLI("agents", "--all")
	require.Equal(t, 0, code, errOut)
	lines = strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 3)
	assert.Regexp(t, `^platform-engineer\s+-\s+removed\s+codex\s+-\s+-\s+fixes friction$`, lines[2])
}
