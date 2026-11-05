package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskListShowsQueuePositionAndSort(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "created", r.URL.Query().Get("sort"))
		writeJSON(w, http.StatusOK, []map[string]any{
			{"id": "t-run", "status": "running", "kind": "feature", "project": "a", "title": "r"},
			{"id": "t-1", "status": "queued", "kind": "feature", "project": "a", "title": "n", "queue_position": 1, "project_queue_position": 1},
			{"id": "t-2", "status": "queued", "kind": "feature", "project": "b", "title": "m", "queue_position": 2, "project_queue_position": 1},
			{"id": "t-w", "status": "queued", "kind": "feature", "project": "b", "title": "w", "waiting_on": []string{"t-run"}},
		})
	})
	fakeAPI(t, mux)

	code, out, errOut := runCLI("task", "list", "--sort", "created")
	require.Equal(t, 0, code, errOut)
	assert.Regexp(t, `t-1\s+queued\s+#1 next\s+feature`, out)
	assert.Regexp(t, `t-2\s+queued\s+#2 \(#1 in project\)\s+feature`, out)
	assert.Regexp(t, `t-w\s+waiting\s+after t-run\s+feature`, out)
}
