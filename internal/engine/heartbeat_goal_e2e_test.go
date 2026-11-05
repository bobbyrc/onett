package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
	"github.com/bobbyrc/onett/internal/notify"
)

// hbE2E wires the real Operations waker to a fake orchestrator harness, so a tick walks the whole
// path: digest, hidden internal message, orchestrator turn, reply, memory, escalation.
type hbE2E struct {
	*hbMemEnv
	mu      sync.Mutex
	prompts []string
	replyTo string // what the fake orchestrator answers next
}

func hbE2ENew(t *testing.T) *hbE2E {
	t.Helper()
	h := &hbE2E{hbMemEnv: hbMemNew(t, "")}
	h.e.d.Heartbeat = operationsWaker{h.e}
	h.e.d.HarnessFor = func(string, config.Harness) (harness.Harness, error) {
		return hfake.New(func(_ context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.prompts = append(h.prompts, spec.Prompt)
			return harness.Result{SessionID: "s", FinalText: h.replyTo}, nil
		}), nil
	}
	return h
}

// settle waits for the orchestrator turn of the last wake to finish.
func (h *hbE2E) settle() {
	h.t.Helper()
	require.Eventually(h.t, func() bool {
		c, err := h.st.GetConversation(h.ctx, h.conv.ID)
		return err == nil && !c.Busy
	}, 10*time.Second, 5*time.Millisecond)
	h.e.wg.Wait()
}

func (h *hbE2E) turns() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.prompts)
}

func (h *hbE2E) internalMessages() []string {
	h.t.Helper()
	msgs, err := h.st.ListChatMessages(h.ctx, h.conv.ID)
	require.NoError(h.t, err)
	var out []string
	for _, m := range msgs {
		if m.Kind == domain.ChatKindInternal {
			out = append(out, m.Text)
		}
	}
	return out
}

func TestHeartbeatGoalAcceptancePath(t *testing.T) {
	h := hbE2ENew(t)

	// a) B waits on A, which has landed; no goal, so the heartbeat owns it.
	a := lpRetroTask(t, h.lpEnv, domain.Task{Title: "A", Status: domain.TaskMerged})
	b := lpRetroTask(t, h.lpEnv, domain.Task{Title: "B", Status: domain.TaskQueued, WaitingOn: []string{a.ID}})
	h.replyTo = fmt.Sprintf("Looked at it.\n%s: retried with note \"dependency A merged\"", b.ID)
	h.tick()
	h.settle()
	require.Equal(t, 1, h.turns())
	internal := h.internalMessages()
	require.Len(t, internal, 1)
	assert.Contains(t, internal[0], "dependency")
	assert.Contains(t, internal[0], b.ID)

	// b) the reply line is recorded against B's item on the next tick.
	h.tick()
	var key string
	for k, m := range h.memory().Items {
		if m.EntityID == b.ID {
			key = k
			require.Len(t, m.Actions, 1)
			assert.Contains(t, m.Actions[0], `retried with note "dependency A merged"`)
		}
	}
	require.NotEmpty(t, key)

	// d) an unchanged digest makes no second orchestrator turn.
	assert.Equal(t, 1, h.turns())
	assert.Len(t, h.internalMessages(), 1)

	// A new item wakes the orchestrator again, and its prompt lists the earlier action.
	c := h.blocked("C")
	h.tick()
	h.settle()
	require.Equal(t, 2, h.turns())
	assert.Contains(t, h.prompts[1], "previously: ")
	assert.Contains(t, h.prompts[1], "dependency A merged")
	assert.Contains(t, h.prompts[1], c.ID)

	// c) B has now been open for escalate_after (3) ticks: exactly one high-priority note.
	var forB []notify.Notification
	for _, n := range h.notes {
		if strings.Contains(n.Title, b.ID) {
			forB = append(forB, n)
		}
	}
	require.Len(t, forB, 1)
	assert.Equal(t, notify.PriorityHigh, forB[0].Priority)
	assert.Contains(t, forB[0].Body, "dependency A merged")
	h.tick()
	h.tick()
	forB = nil
	for _, n := range h.notes {
		if strings.Contains(n.Title, b.ID) {
			forB = append(forB, n)
		}
	}
	assert.Len(t, forB, 1, "further ticks send nothing more for B")
}

func TestHeartbeatLeavesActiveGoalWorkToItsOrchestrator(t *testing.T) {
	h := hbMemNew(t, "")
	goal, err := h.st.CreateGoal(h.ctx, domain.Goal{Project: "demo", Title: "G", Status: domain.GoalActive})
	require.NoError(t, err)
	dep := lpRetroTask(t, h.lpEnv, domain.Task{Title: "dep", Status: domain.TaskMerged})
	blocked := lpRetroTask(t, h.lpEnv, domain.Task{Title: "in goal", Status: domain.TaskBlocked, BlockedReason: "stuck", GoalID: goal.ID})
	failed := lpRetroTask(t, h.lpEnv, domain.Task{Title: "failed in goal", Status: domain.TaskFailed, BlockedReason: "red", GoalID: goal.ID})
	waits := lpRetroTask(t, h.lpEnv, domain.Task{Title: "waits in goal", Status: domain.TaskQueued, WaitingOn: []string{dep.ID}, GoalID: goal.ID})
	stale := lpRetroTask(t, h.lpEnv, domain.Task{Title: "stale in goal", Status: domain.TaskQueued, UpdatedAt: h.now.Add(-24 * time.Hour), GoalID: goal.ID})
	asks := lpRetroTask(t, h.lpEnv, domain.Task{Title: "asks in goal", Status: domain.TaskReview, Summary: "Done, but this needs a human to pick the name.", GoalID: goal.ID})
	free := lpRetroTask(t, h.lpEnv, domain.Task{Title: "no goal", Status: domain.TaskBlocked, BlockedReason: "stuck"})

	h.tick()
	require.Len(t, h.rec.digests, 1)
	d := h.rec.digests[0]
	for _, id := range []string{blocked.ID, failed.ID, waits.ID, stale.ID, asks.ID} {
		for _, it := range d.Items {
			assert.NotEqual(t, id, it.EntityID, "active goal's work is not the heartbeat's")
		}
	}
	assert.True(t, hbHas(d, hbBlockedTask, free.ID))

	// Once the goal is blocked, its tasks are the heartbeat's again, and so is the goal.
	_, err = h.st.UpdateGoal(h.ctx, goal.ID, func(g *domain.Goal) error {
		g.Status, g.Summary = domain.GoalBlocked, "needs the owner"
		return nil
	})
	require.NoError(t, err)
	h.tick()
	require.Len(t, h.rec.digests, 2)
	d = h.rec.digests[1]
	assert.True(t, hbHas(d, hbBlockedTask, blocked.ID))
	assert.True(t, hbHas(d, hbFailedTask, failed.ID))
	assert.True(t, hbHas(d, hbDependency, waits.ID))
	assert.True(t, hbHas(d, hbStaleQueued, stale.ID))
	assert.True(t, hbHas(d, hbBlockedGoal, goal.ID))
}
