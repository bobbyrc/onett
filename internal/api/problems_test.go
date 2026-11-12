package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProblems is what the fake controller's Problems returns. A test sets it and clears it in
// t.Cleanup.
var fakeProblems []Problem

// Problems returns fakeProblems, for the GET /problems tests.
func (f *fakeCtl) Problems(context.Context) ([]Problem, error) {
	return fakeProblems, nil
}

func TestProblemsRouteListsProblems(t *testing.T) {
	x := newHarness(t)
	since := time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC)
	fakeProblems = []Problem{
		{Kind: ProblemBlockedTasks, Project: "onett", Key: "blocked_tasks:onett:x", Title: "Workspace setup failed",
			Detail: "fetch failed", Count: 2, Since: since, LastAt: &since, ToldAt: &since,
			Tasks: []ProblemTask{{ID: "t1", Title: "A", Attempts: 3, MaxAttempts: 3}}},
		{Kind: ProblemBlockedGoal, Project: "onett", Key: "blocked_goal:g1", Title: "Goal", Count: 1, Since: since, GoalID: "g1"},
	}
	t.Cleanup(func() { fakeProblems = nil })

	rec := x.call(t, http.MethodGet, "/api/v1/problems", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var raw []map[string]any
	decode(t, rec, &raw)
	require.Len(t, raw, 2)
	assert.Equal(t, "blocked_tasks", raw[0]["kind"])
	assert.Contains(t, raw[0], "last_at")
	assert.Contains(t, raw[0], "told_at")
	assert.Contains(t, raw[0], "tasks")
	assert.NotContains(t, raw[0], "goal_id")
	assert.NotContains(t, raw[0], "loops")
	assert.Equal(t, "blocked_goal", raw[1]["kind"])
	assert.Equal(t, "g1", raw[1]["goal_id"])
	for _, k := range []string{"last_at", "told_at", "tasks", "loops"} {
		assert.NotContains(t, raw[1], k)
	}
}

func TestProblemsRouteIsEmptyArrayWhenNothingIsWrong(t *testing.T) {
	x := newHarness(t)
	rec := x.call(t, http.MethodGet, "/api/v1/problems", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, "[]", rec.Body.String())
}

func TestProblemsRouteNeedsSignIn(t *testing.T) {
	x := newHarness(t)
	rec := x.do(t, http.MethodGet, "/api/v1/problems", "", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
