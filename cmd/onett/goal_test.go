package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskCreateGoalDefaultsFromEnv(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		got = nil
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		writeJSON(w, http.StatusCreated, map[string]any{"id": "task-1", "title": "t"})
	})
	mux.HandleFunc("GET /api/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"paused": false})
	})
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
	fakeAPI(t, mux)

	t.Setenv("ONETT_GOAL", "goal-env")
	code, _, errOut := runCLI(createArgs()...)
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "goal-env", got["goal_id"], "ONETT_GOAL links the task to the goal")

	code, _, errOut = runCLI(createArgs("--goal", "goal-flag")...)
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "goal-flag", got["goal_id"], "--goal wins over the environment")

	t.Setenv("ONETT_GOAL", "")
	code, _, errOut = runCLI(createArgs()...)
	require.Equal(t, 0, code, errOut)
	assert.NotContains(t, got, "goal_id", "no goal, no field")
}

func TestGoalCommands(t *testing.T) {
	var created, finished map[string]any
	var actions []string
	goal := map[string]any{"id": "g1", "project": "onett", "title": "Ship", "body": "all of it",
		"status": "active", "conversation_id": "c1", "turns": 2}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/goals", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&created))
		writeJSON(w, http.StatusCreated, goal)
	})
	mux.HandleFunc("GET /api/v1/goals", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "active", r.URL.Query().Get("status"))
		writeJSON(w, http.StatusOK, []any{goal})
	})
	mux.HandleFunc("GET /api/v1/goals/g1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"goal": goal, "tasks": []any{
			map[string]any{"id": "t1", "status": "done", "kind": "chore", "title": "step one", "pr_number": 7},
		}})
	})
	for _, verb := range []string{"pause", "resume", "done", "block"} {
		mux.HandleFunc("POST /api/v1/goals/g1/"+verb, func(w http.ResponseWriter, r *http.Request) {
			actions = append(actions, verb)
			if verb == "done" || verb == "block" {
				finished = nil
				require.NoError(t, json.NewDecoder(r.Body).Decode(&finished))
			}
			writeJSON(w, http.StatusOK, map[string]any{"id": "g1", "status": verb})
		})
	}
	fakeAPI(t, mux)

	code, out, errOut := runCLI("goal", "create", "--project", "onett", "--title", "Ship", "--body", "all of it")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "g1\n", out)
	assert.Equal(t, map[string]any{"project": "onett", "title": "Ship", "body": "all of it"}, created)

	code, out, errOut = runCLI("goal", "list", "--status", "active")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "g1")
	assert.Contains(t, out, "Ship")

	code, out, errOut = runCLI("goal", "show", "g1")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "Goal g1: Ship")
	assert.Contains(t, out, "step one")
	assert.Contains(t, out, "#7")

	for _, verb := range []string{"pause", "resume"} {
		code, _, errOut = runCLI("goal", verb, "g1")
		require.Equal(t, 0, code, errOut)
	}
	code, _, errOut = runCLI("goal", "done", "g1", "--summary", "all merged")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, map[string]any{"summary": "all merged"}, finished)
	code, _, errOut = runCLI("goal", "block", "g1", "--reason", "needs a key")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, map[string]any{"reason": "needs a key"}, finished)
	assert.Equal(t, []string{"pause", "resume", "done", "block"}, actions)

	code, _, errOut = runCLI("goal", "done", "g1")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "--summary")
	code, _, errOut = runCLI("goal", "create", "--project", "onett")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "--title")
}

func TestGoalHelp(t *testing.T) {
	for _, flag := range []string{"-h", "help"} {
		code, out, errOut := runCLI("goal", flag)
		require.Equal(t, 0, code, errOut)
		assert.Contains(t, out, "usage: onett goal")
	}
}
