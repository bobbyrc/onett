package main

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResumeDoesNotSayResumedWhenRefused(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/control/resume", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "only a person can do this: resuming the factory is the owner's decision",
		})
	})
	fakeAPI(t, mux)

	var stdout, stderr bytes.Buffer
	code := run([]string{"resume"}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.NotContains(t, stdout.String(), "resumed")
	assert.Contains(t, stderr.String(), "only a person can do this")
}

func TestPauseSaysPausedOnlyOnSuccess(t *testing.T) {
	t.Run("success prints the paused line", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("POST /api/v1/control/pause", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"paused": true, "pause": nil})
		})
		fakeAPI(t, mux)

		var stdout, stderr bytes.Buffer
		code := run([]string{"pause"}, &stdout, &stderr)
		require.Equal(t, 0, code, stderr.String())
		assert.Contains(t, stdout.String(), "paused:")
	})

	t.Run("refusal does not print the paused line", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("POST /api/v1/control/pause", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
		})
		fakeAPI(t, mux)

		var stdout, stderr bytes.Buffer
		code := run([]string{"pause"}, &stdout, &stderr)
		assert.Equal(t, 1, code)
		assert.NotContains(t, stdout.String(), "paused:")
	})
}
