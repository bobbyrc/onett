package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createdSource runs `task create` against a fake API and returns the source it sent.
func createdSource(t *testing.T, extra ...string) string {
	t.Helper()
	var got string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		got, _ = in["source"].(string)
		writeJSON(w, http.StatusCreated, map[string]any{"id": "task-7", "title": "t"})
	})
	mux.HandleFunc("GET /api/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"paused": false})
	})
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	fakeAPI(t, mux)
	code, _, errOut := runCLI(createArgs(extra...)...)
	require.Equal(t, 0, code, errOut)
	return got
}

func TestTaskCreateSourceFromConversation(t *testing.T) {
	t.Setenv("ONETT_CONVERSATION_ID", "conv-1")
	assert.Equal(t, "chat:conv-1", createdSource(t))
}

func TestTaskCreateExplicitSourceWins(t *testing.T) {
	t.Setenv("ONETT_CONVERSATION_ID", "conv-1")
	assert.Equal(t, "retro", createdSource(t, "--source", "retro"))
}

func TestTaskCreateSourceDefaultsToAPI(t *testing.T) {
	t.Setenv("ONETT_CONVERSATION_ID", "")
	assert.Equal(t, "api", createdSource(t))
}
