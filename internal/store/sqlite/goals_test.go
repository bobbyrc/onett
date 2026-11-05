package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

func TestGoalCrud(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	g, err := s.CreateGoal(ctx, domain.Goal{Project: "p", Title: "ship", Body: "b", Status: domain.GoalActive})
	require.NoError(t, err)
	assert.NotEmpty(t, g.ID)
	assert.False(t, g.CreatedAt.IsZero())

	got, err := s.GetGoal(ctx, g.ID)
	require.NoError(t, err)
	assert.Equal(t, "ship", got.Title)

	_, err = s.GetGoal(ctx, "nope")
	assert.ErrorIs(t, err, store.ErrNotFound)

	turn := time.Now().UTC()
	up, err := s.UpdateGoal(ctx, g.ID, func(x *domain.Goal) error {
		x.Turns, x.LastTurnAt, x.Status, x.Summary = 3, &turn, domain.GoalBlocked, "why"
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 3, up.Turns)
	assert.True(t, up.UpdatedAt.After(g.UpdatedAt) || up.UpdatedAt.Equal(g.UpdatedAt))
	got, err = s.GetGoal(ctx, g.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastTurnAt)
	assert.True(t, turn.Equal(*got.LastTurnAt))
	assert.Equal(t, domain.GoalBlocked, got.Status)

	_, err = s.UpdateGoal(ctx, "nope", func(*domain.Goal) error { return nil })
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestListGoalsFilters(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	for _, g := range []domain.Goal{
		{Project: "a", Title: "1", Status: domain.GoalActive},
		{Project: "a", Title: "2", Status: domain.GoalDone},
		{Project: "b", Title: "3", Status: domain.GoalActive},
	} {
		_, err := s.CreateGoal(ctx, g)
		require.NoError(t, err)
	}

	all, err := s.ListGoals(ctx, store.GoalFilter{})
	require.NoError(t, err)
	assert.Len(t, all, 3)
	assert.Equal(t, "3", all[0].Title, "newest first")

	active, err := s.ListGoals(ctx, store.GoalFilter{Project: "a", Statuses: []domain.GoalStatus{domain.GoalActive}})
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, "1", active[0].Title)
}

func TestTasksByGoal(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	mk := func(goal string) domain.Task {
		tk, err := s.CreateTask(ctx, domain.Task{Project: "p", Kind: domain.KindChore, Title: "t", Status: domain.TaskQueued, GoalID: goal})
		require.NoError(t, err)
		return tk
	}
	in := mk("g1")
	mk("g2")
	mk("")

	got, err := s.ListTasks(ctx, store.TaskFilter{GoalID: "g1"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, in.ID, got[0].ID)
	assert.Equal(t, "g1", got[0].GoalID)

	// A task with no goal is stored as NULL, and an update keeps the column in step.
	var nulls int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE goal_id IS NULL`).Scan(&nulls))
	assert.Equal(t, 1, nulls)
	_, err = s.UpdateTask(ctx, in.ID, func(tk *domain.Task) error { tk.GoalID = "g2"; return nil })
	require.NoError(t, err)
	got, err = s.ListTasks(ctx, store.TaskFilter{GoalID: "g2"})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

// TestGoalMigrationKeepsExistingTasks applies every migration before the goals one, stores a
// task, then migrates on: the old row must survive with a NULL goal_id.
func TestGoalMigrationKeepsExistingTasks(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	// Undo the goals migration and every later one (the goals migration is not the last any more).
	goalsVersion := 0
	for i, m := range migrations {
		if strings.Contains(m, "CREATE TABLE goals") {
			goalsVersion = i + 1
		}
	}
	require.NotZero(t, goalsVersion)
	_, err := s.db.ExecContext(ctx, `DROP TABLE goals; DROP INDEX tasks_goal_id; ALTER TABLE tasks DROP COLUMN goal_id;
		DROP TABLE attachments; DROP TABLE chat_message_attachments;
		DELETE FROM schema_migrations WHERE version >= ?`, goalsVersion)
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, `INSERT INTO tasks (id, project, status, kind, priority, created_at, data)
		VALUES ('old', 'p', 'queued', 'chore', 0, 1, '{"id":"old","project":"p","kind":"chore","title":"old","status":"queued"}')`)
	require.NoError(t, err)

	require.NoError(t, migrate(ctx, s.db))

	old, err := s.GetTask(ctx, "old")
	require.NoError(t, err)
	assert.Empty(t, old.GoalID)
	var isNull int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id = 'old' AND goal_id IS NULL`).Scan(&isNull))
	assert.Equal(t, 1, isNull)
	_, err = s.CreateGoal(ctx, domain.Goal{Project: "p", Title: "g", Status: domain.GoalActive})
	require.NoError(t, err)
}
