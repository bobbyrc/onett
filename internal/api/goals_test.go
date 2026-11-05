package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func (f *fakeCtl) CreateGoal(_ context.Context, g domain.Goal) (domain.Goal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.note("CreateGoal"); err != nil {
		return domain.Goal{}, err
	}
	g.ID, g.Status = "goal-new", domain.GoalActive
	return g, nil
}

func (f *fakeCtl) SetGoalStatus(_ context.Context, id string, to domain.GoalStatus, text string) (domain.Goal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.note("SetGoalStatus"); err != nil {
		return domain.Goal{}, err
	}
	f.chatText = string(to) + "|" + text
	return domain.Goal{ID: id, Status: to, Summary: text}, nil
}

func TestGoalEndpoints(t *testing.T) {
	x := newHarness(t)
	ctx := context.Background()

	rec := x.call(t, http.MethodPost, "/api/v1/goals", `{"project":"onett","title":"Ship it","body":"b"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created domain.Goal
	decode(t, rec, &created)
	assert.Equal(t, "Ship it", created.Title)
	assert.Equal(t, domain.GoalActive, created.Status)

	rec = x.call(t, http.MethodPost, "/api/v1/goals", `{"project":"onett","title":"x","bogus":1}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "unknown fields are errors")

	g, err := x.st.CreateGoal(ctx, domain.Goal{Project: "onett", Title: "Stored", Status: domain.GoalActive})
	require.NoError(t, err)
	task := x.makeTask(t, "onett", domain.TaskQueued, "")
	_, err = x.st.UpdateTask(ctx, task.ID, func(t *domain.Task) error { t.GoalID = g.ID; return nil })
	require.NoError(t, err)
	x.makeTask(t, "onett", domain.TaskQueued, "") // not in the goal

	rec = x.call(t, http.MethodGet, "/api/v1/goals?status=active", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var list []domain.Goal
	decode(t, rec, &list)
	require.Len(t, list, 1)
	assert.Equal(t, g.ID, list[0].ID)

	rec = x.call(t, http.MethodGet, "/api/v1/goals?status=done", "")
	decode(t, rec, &list)
	assert.Empty(t, list)

	rec = x.call(t, http.MethodGet, "/api/v1/goals/"+g.ID, "")
	require.Equal(t, http.StatusOK, rec.Code)
	var detail GoalDetail
	decode(t, rec, &detail)
	assert.Equal(t, g.ID, detail.Goal.ID)
	require.Len(t, detail.Tasks, 1)
	assert.Equal(t, task.ID, detail.Tasks[0].ID)
	assert.Equal(t, g.ID, detail.Tasks[0].GoalID)

	assert.Equal(t, http.StatusNotFound, x.call(t, http.MethodGet, "/api/v1/goals/nope", "").Code)

	for path, want := range map[string]string{
		"pause":  "paused|",
		"resume": "active|",
		"done":   "done|all merged",
		"block":  "blocked|needs a key",
	} {
		body := ""
		switch path {
		case "done":
			body = `{"summary":"all merged"}`
		case "block":
			body = `{"reason":"needs a key"}`
		}
		rec = x.call(t, http.MethodPost, "/api/v1/goals/"+g.ID+"/"+path, body)
		require.Equal(t, http.StatusOK, rec.Code, path+": "+rec.Body.String())
		assert.Equal(t, want, x.ctl.chatText)
	}
}

func TestGoalTaskCreateCarriesGoalID(t *testing.T) {
	x := newHarness(t)
	rec := x.call(t, http.MethodPost, "/api/v1/tasks", `{"project":"onett","title":"x","goal_id":"g1"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "g1", x.ctl.created.GoalID)
}

func TestGoalAuthLevels(t *testing.T) {
	x := newHarness(t)
	viewer := x.newUserSession(t, "watcher", domain.RoleViewer)

	assert.Equal(t, http.StatusOK, x.authReq(t, http.MethodGet, "/api/v1/goals", "", withCookie(viewer)).Code)
	for _, p := range []string{"", "/g1/pause", "/g1/resume", "/g1/done", "/g1/block"} {
		rec := x.authReq(t, http.MethodPost, "/api/v1/goals"+p, `{"summary":"s","reason":"r"}`, withCookie(viewer))
		assert.Equal(t, http.StatusForbidden, rec.Code, "viewer POST /goals"+p)
	}
	assert.Empty(t, x.ctl.calls)

	assert.Equal(t, http.StatusUnauthorized, x.do(t, http.MethodGet, "/api/v1/goals", "", "").Code)

	op := x.newUserSession(t, "ops", domain.RoleOperator)
	rec := x.authReq(t, http.MethodPost, "/api/v1/goals/g1/pause", "", withCookie(op))
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = x.authReq(t, http.MethodPost, "/api/v1/goals/g1/done", `{"summary":"s"}`, withToken(testToken))
	assert.Equal(t, http.StatusOK, rec.Code, "the token holds operate")
}
