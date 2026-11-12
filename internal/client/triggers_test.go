package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTriggersDecodesTheListWithLastFiring(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`[{"id":"tr1","name":"Morning","kind":"schedule","project":"onett",
			"enabled":true,"cron":"0 5 * * *","next_run_at":"2026-11-13T05:00:00Z",
			"last_firing":{"id":"f1","trigger_id":"tr1","cause":"schedule","outcome":"started"}}]`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "tok").Triggers(context.Background())
	require.NoError(t, err)
	assert.Equal(t, http.MethodGet, method)
	assert.Equal(t, "/api/v1/triggers", path)
	require.Len(t, got, 1)
	assert.Equal(t, "Morning", got[0].Name)
	assert.Equal(t, "schedule", string(got[0].Kind))
	assert.Equal(t, "0 5 * * *", got[0].Cron)
	assert.Equal(t, time.Date(2026, 11, 13, 5, 0, 0, 0, time.UTC), *got[0].NextRunAt)
	require.NotNil(t, got[0].LastFiring)
	assert.Equal(t, "started", got[0].LastFiring.Outcome)
}

func TestTriggerFiringsSendsLimitOnlyWhenSet(t *testing.T) {
	var escaped, rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escaped, rawQuery = r.URL.EscapedPath(), r.URL.RawQuery
		_, _ = w.Write([]byte(`[{"id":"f1","cause":"github_issue","issue_number":12,"outcome":"skipped"}]`))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")

	got, err := c.TriggerFirings(context.Background(), "abc/d", 0)
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/triggers/abc%2Fd/firings", escaped)
	assert.Empty(t, rawQuery, "limit 0 sends no limit")
	require.Len(t, got, 1)
	assert.Equal(t, 12, got[0].IssueNumber)
	assert.Equal(t, "skipped", got[0].Outcome)

	_, err = c.TriggerFirings(context.Background(), "abc/d", 5)
	require.NoError(t, err)
	assert.Equal(t, "limit=5", rawQuery)
}

func TestTriggersReportsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
	}))
	defer srv.Close()

	_, err := New(srv.URL, "tok").Triggers(context.Background())
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.Status)
}
