package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskCreateAttachIsRepeatable(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		got = nil
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		writeJSON(w, http.StatusCreated, map[string]any{"id": "task-7"})
	})
	mux.HandleFunc("GET /api/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"paused": false})
	})
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) })
	fakeAPI(t, mux)

	code, _, errOut := runCLI(createArgs("--attach", "a1", "--attach", "a2")...)
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, []any{"a1", "a2"}, got["attachments"])

	code, _, errOut = runCLI(createArgs()...)
	require.Equal(t, 0, code, errOut)
	assert.NotContains(t, got, "attachments")
}
