package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

// A blocked task of an active goal is the orchestrator's to handle first, so the owner is not
// pinged; once the goal is no longer active, or for a task with no goal, the owner is told.
func TestBlockTaskDefersOwnerNotificationToActiveGoal(t *testing.T) {
	ctx := context.Background()
	g := newGoalRig(t)
	goal := g.newGoal(t)

	block := func(goalID string) domain.Task {
		task, err := g.e.CreateTask(ctx, domain.Task{Project: "demo", Kind: domain.KindChore, Title: "step", GoalID: goalID})
		require.NoError(t, err)
		g.e.blockTask(ctx, task.ID, "stuck")
		got, err := g.st.GetTask(ctx, task.ID)
		require.NoError(t, err)
		assert.Equal(t, domain.TaskBlocked, got.Status, "the task is blocked either way")
		return got
	}

	block(goal.ID)
	assert.Empty(t, g.notified(), "active goal")

	block("")
	assert.Len(t, g.notified(), 1, "no goal")

	_, err := g.e.SetGoalStatus(ctx, goal.ID, domain.GoalPaused, "")
	require.NoError(t, err)
	block(goal.ID)
	assert.Len(t, g.notified(), 2, "paused goal gets no turns")
}
