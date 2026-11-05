package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

// TestListTasksPickupOrder checks the default order: running, in progress, the queue in claim
// order with positions, waiting, blocked, then finished newest first; sort=created is the old order.
func TestListTasksPickupOrder(t *testing.T) {
	x := newHarness(t)
	base := time.Now().Add(-time.Hour)
	mk := func(id, project string, st domain.TaskStatus, prio int, age time.Duration, waiting ...string) {
		t.Helper()
		at := base.Add(age)
		_, err := x.st.CreateTask(context.Background(), domain.Task{
			ID: id, Project: project, Kind: domain.KindFeature, Title: id, Status: st, Priority: prio,
			Source: "api", CreatedAt: at, UpdatedAt: at, WaitingOn: waiting,
		})
		require.NoError(t, err)
	}
	mk("done-old", "a", domain.TaskDone, 0, 1*time.Minute)
	mk("done-new", "a", domain.TaskDone, 0, 9*time.Minute)
	mk("blocked", "a", domain.TaskBlocked, 0, 2*time.Minute)
	mk("q-low-old", "a", domain.TaskQueued, 0, 3*time.Minute)
	mk("q-low-new", "b", domain.TaskQueued, 0, 4*time.Minute)
	mk("q-high", "b", domain.TaskQueued, 5, 8*time.Minute)
	mk("waiter", "a", domain.TaskQueued, 9, 0, "run-late")
	mk("review", "a", domain.TaskReview, 0, 5*time.Minute)
	mk("run-late", "a", domain.TaskRunning, 0, 6*time.Minute)
	mk("run-early", "b", domain.TaskRunning, 0, 7*time.Minute)
	for id, at := range map[string]time.Time{"run-early": base, "run-late": base.Add(time.Minute)} {
		_, err := x.st.CreateRun(context.Background(), domain.Run{ID: "r-" + id, TaskID: id, Status: domain.RunRunning, StartedAt: at})
		require.NoError(t, err)
	}

	var got []taskListItem
	rec := x.call(t, http.MethodGet, "/api/v1/tasks", "")
	require.Equal(t, http.StatusOK, rec.Code)
	decode(t, rec, &got)
	var ids []string
	pos := map[string][2]int{}
	for _, tk := range got {
		ids = append(ids, tk.ID)
		pos[tk.ID] = [2]int{tk.QueuePosition, tk.ProjectQueuePosition}
	}
	assert.Equal(t, []string{"run-early", "run-late", "review", "q-high", "q-low-old", "q-low-new",
		"waiter", "blocked", "done-new", "done-old"}, ids)
	assert.Equal(t, [2]int{1, 1}, pos["q-high"])
	assert.Equal(t, [2]int{2, 1}, pos["q-low-old"], "first of project a, second overall")
	assert.Equal(t, [2]int{3, 2}, pos["q-low-new"])
	assert.Equal(t, [2]int{0, 0}, pos["waiter"], "a waiting task has no queue position")

	// A filter keeps the positions of the whole queue.
	rec = x.call(t, http.MethodGet, "/api/v1/tasks?project=a&status=queued", "")
	require.Equal(t, http.StatusOK, rec.Code)
	got = nil
	decode(t, rec, &got)
	require.Len(t, got, 2)
	assert.Equal(t, "q-low-old", got[0].ID)
	assert.Equal(t, 2, got[0].QueuePosition)

	rec = x.call(t, http.MethodGet, "/api/v1/tasks?limit=2", "")
	got = nil
	decode(t, rec, &got)
	assert.Len(t, got, 2)

	rec = x.call(t, http.MethodGet, "/api/v1/tasks?sort=created", "")
	require.Equal(t, http.StatusOK, rec.Code)
	got = nil
	decode(t, rec, &got)
	assert.Equal(t, "done-new", got[0].ID, "newest created first")

	rec = x.call(t, http.MethodGet, "/api/v1/tasks?sort=bogus", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
