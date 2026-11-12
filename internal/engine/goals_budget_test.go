package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

// The goals loop reaches its budget gate and the gate holds: with today's cost at the budget, a
// goal that is due gets no turn, whether the cost is from a finished run or from one still in
// flight. Once the budget is raised it gets its turn.
func TestGoalGetsNoTurnOnceDailyBudgetIsSpent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status domain.RunStatus
	}{
		{"finished run", domain.RunSucceeded},
		{"run in flight", domain.RunRunning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGoalRig(t)
			g.cfg.Limits.DailyBudgetUSD = 1
			g.act = g.createsTask(t)
			goal := g.newGoal(t)
			_, err := g.st.CreateRun(context.Background(), domain.Run{TaskID: "earlier", Project: "demo",
				Status: tc.status, CostUSD: 5, StartedAt: time.Now()})
			require.NoError(t, err)

			g.tick(t)
			g.tick(t)

			assert.Equal(t, 0, g.turns(), "budget spent: no turn")
			got := g.goal(t, goal.ID)
			assert.Equal(t, 0, got.Turns)
			assert.Equal(t, domain.GoalActive, got.Status, "the goal waits; it is not blocked")
			conv, err := g.st.GetConversation(context.Background(), goal.ConversationID)
			require.NoError(t, err)
			assert.False(t, conv.Busy)
			// The budget check itself sends this notice, so it shows the loop got as far as the gate.
			notes := g.notified()
			require.Len(t, notes, 1, "one notice across two ticks")
			assert.Equal(t, "Daily budget reached", notes[0].Title)

			g.cfg.Limits.DailyBudgetUSD = 10
			g.tick(t)
			assert.Equal(t, 1, g.turns(), "budget raised: the goal gets its turn")
		})
	}
}
