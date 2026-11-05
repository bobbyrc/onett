package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestGoalResumeAfterTurnLimitGetsFreshBudget(t *testing.T) {
	g := newGoalRig(t)
	g.cfg.Limits.MaxGoalTurns = 1
	g.act = g.createsTask(t)
	goal := g.newGoal(t)
	g.tick(t)
	require.Equal(t, domain.GoalBlocked, g.goal(t, goal.ID).Status)

	resumed, err := g.e.SetGoalStatus(context.Background(), goal.ID, domain.GoalActive, "")
	require.NoError(t, err)
	assert.Equal(t, domain.GoalActive, resumed.Status)
	assert.Equal(t, 0, resumed.Turns, "resuming a blocked goal resets its turn budget")
}

func TestGoalResumeFromPauseKeepsTurns(t *testing.T) {
	g := newGoalRig(t)
	g.act = g.createsTask(t)
	goal := g.newGoal(t)
	g.tick(t)
	ctx := context.Background()
	_, err := g.e.SetGoalStatus(ctx, goal.ID, domain.GoalPaused, "")
	require.NoError(t, err)
	resumed, err := g.e.SetGoalStatus(ctx, goal.ID, domain.GoalActive, "")
	require.NoError(t, err)
	assert.Equal(t, 1, resumed.Turns)
}
