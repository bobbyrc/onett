package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statusMux answers the routes `onett status` reads, with the daemon's health route at 1.2.3.
func statusMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"paused": true})
	})
	mux.HandleFunc("GET /api/v1/control", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"paused": true})
	})
	mux.HandleFunc("GET /api/v1/approvals", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
	mux.HandleFunc("GET /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
	mux.HandleFunc("GET /api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "1.2.3"})
	})
	return mux
}

func TestStatusVersionPrinted(t *testing.T) {
	fakeAPI(t, statusMux())

	code, out, errOut := runCLI("status")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "Daemon: version 1.2.3\n")
	assert.True(t, strings.HasPrefix(out, "Factory:"), out)
}

func TestStatusVersionInJSON(t *testing.T) {
	fakeAPI(t, statusMux())

	code, out, errOut := runCLI("status", "--json")
	require.Equal(t, 0, code, errOut)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	assert.Equal(t, "1.2.3", decoded["version"])
}

func TestStatusVersionOmittedWhenUnknown(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeStatus(&buf, statusSnapshot{}))
	assert.NotContains(t, buf.String(), "Daemon:")
}
