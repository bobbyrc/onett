package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
)

const secondChanceText = "Your last turn made no progress"

func TestGoalFirstNoProgressTurnGetsSecondChance(t *testing.T) {
	g := newGoalRig(t) // the orchestrator does nothing
	goal := g.newGoal(t)

	g.tick(t)
	got := g.goal(t, goal.ID)
	assert.Equal(t, domain.GoalActive, got.Status)
	assert.Equal(t, 1, got.StalledTurns)
	assert.Empty(t, g.notified())

	// Nothing changed, yet the next tick runs a turn that opens with the warning.
	g.tick(t)
	require.Equal(t, 2, g.turns())
	assert.NotContains(t, g.prompts[0], secondChanceText)
	assert.Contains(t, g.prompts[1], secondChanceText)

	// The second consecutive stall blocks with the usual reason.
	got = g.goal(t, goal.ID)
	assert.Equal(t, domain.GoalBlocked, got.Status)
	assert.Equal(t, "the orchestrator made no progress on its last turn", got.Summary)
	require.Len(t, g.notified(), 1)
}

func TestGoalProgressResetsStall(t *testing.T) {
	g := newGoalRig(t)
	goal := g.newGoal(t)
	g.tick(t) // stalls
	require.Equal(t, 1, g.goal(t, goal.ID).StalledTurns)

	g.act = g.createsTask(t)
	g.tick(t)
	got := g.goal(t, goal.ID)
	assert.Equal(t, domain.GoalActive, got.Status)
	assert.Equal(t, 0, got.StalledTurns)

	// One more stall later is a first stall again, not a block.
	task := g.tasksOf(t, goal.ID)[0]
	ctSetTask(t, g.ctRig, task.ID, func(tk *domain.Task) { tk.Status = domain.TaskDone })
	g.act = func(context.Context, harness.Spec) {}
	g.tick(t)
	got = g.goal(t, goal.ID)
	assert.Equal(t, domain.GoalActive, got.Status)
	assert.Equal(t, 1, got.StalledTurns)
}

func TestGoalStallSurvivesReload(t *testing.T) {
	g := newGoalRig(t)
	goal := g.newGoal(t)
	g.tick(t)

	// A fresh engine over the same store has only the persisted state to go on.
	e2 := g.engine(nil)
	got, err := e2.d.Store.GetGoal(context.Background(), goal.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, got.StalledTurns)
	assert.Contains(t, goalCheckIn(got, nil, nil), secondChanceText)
}

func TestBlockedTaskWakesActiveGoal(t *testing.T) {
	g := newGoalRig(t)
	g.act = g.createsTask(t)
	goal := g.newGoal(t)
	g.tick(t)
	task := g.tasksOf(t, goal.ID)[0]

	g.e.blockTask(context.Background(), task.ID, "stuck")
	assert.Empty(t, g.notified(), "the orchestrator hears first")

	g.act = func(ctx context.Context, _ harness.Spec) {
		assert.NoError(t, g.e.RetryTask(ctx, task.ID, "try again"))
	}
	g.tick(t)
	require.Equal(t, 2, g.turns(), "a blocked task settles the goal and wakes the orchestrator")
	assert.Contains(t, g.prompts[1], "stuck")
}
