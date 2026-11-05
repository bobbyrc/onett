package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
	"github.com/bobbyrc/onett/internal/notify"
	"github.com/bobbyrc/onett/internal/store"
)

// goalRig is an engine whose orchestrator is a script, and which records what it was given.
type goalRig struct {
	*ctRig
	e *Engine

	mu      sync.Mutex
	envs    []map[string]string
	prompts []string
	notes   []notify.Notification
	// act is what the orchestrator does in a turn; nil does nothing.
	act func(ctx context.Context, spec harness.Spec)
}

type recordingNotifier struct{ r *goalRig }

func (n recordingNotifier) Notify(_ context.Context, note notify.Notification) error {
	n.r.mu.Lock()
	defer n.r.mu.Unlock()
	n.r.notes = append(n.r.notes, note)
	return nil
}

func newGoalRig(t *testing.T) *goalRig {
	t.Helper()
	g := &goalRig{ctRig: ctNewRig(t)}
	g.e = g.engine(nil)
	g.e.d.Notifier = recordingNotifier{g}
	g.e.d.HarnessFor = func(string, config.Harness) (harness.Harness, error) {
		return hfake.New(func(ctx context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
			g.mu.Lock()
			g.envs = append(g.envs, spec.Env)
			g.prompts = append(g.prompts, spec.Prompt)
			act := g.act
			g.mu.Unlock()
			if act != nil {
				act(ctx, spec)
			}
			return harness.Result{SessionID: "s", FinalText: "did my turn"}, nil
		}), nil
	}
	return g
}

// createsTask makes the orchestrator queue one task the way its CLI does: linked through
// ONETT_GOAL.
func (g *goalRig) createsTask(t *testing.T) func(context.Context, harness.Spec) {
	return func(ctx context.Context, spec harness.Spec) {
		_, err := g.e.CreateTask(ctx, domain.Task{
			Project: "demo", Kind: domain.KindChore, Title: "next step", GoalID: spec.Env["ONETT_GOAL"],
		})
		assert.NoError(t, err)
	}
}

func (g *goalRig) turns() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.prompts)
}

func (g *goalRig) notified() []notify.Notification {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]notify.Notification(nil), g.notes...)
}

// tick runs the goals loop once and waits for the turn it started, and its guard, to finish.
func (g *goalRig) tick(t *testing.T) {
	t.Helper()
	require.NoError(t, g.e.runLoopOnce(context.Background(), loopGoals, "demo", false))
	g.e.wg.Wait()
}

func (g *goalRig) newGoal(t *testing.T) domain.Goal {
	t.Helper()
	goal, err := g.e.CreateGoal(context.Background(), domain.Goal{Project: "demo", Title: "Ship parts", Body: "part 1 then part 2"})
	require.NoError(t, err)
	return goal
}

func (g *goalRig) goal(t *testing.T, id string) domain.Goal {
	t.Helper()
	goal, err := g.st.GetGoal(context.Background(), id)
	require.NoError(t, err)
	return goal
}

func (g *goalRig) tasksOf(t *testing.T, id string) []domain.Task {
	t.Helper()
	tasks, err := g.e.d.Store.ListTasks(context.Background(), storeGoalFilter(id))
	require.NoError(t, err)
	return tasks
}

func TestGoalFirstTurnQueuesLinkedTask(t *testing.T) {
	g := newGoalRig(t)
	g.act = g.createsTask(t)
	goal := g.newGoal(t)
	assert.Equal(t, domain.GoalActive, goal.Status)
	require.NotEmpty(t, goal.ConversationID)
	conv, err := g.st.GetConversation(context.Background(), goal.ConversationID)
	require.NoError(t, err)
	assert.Equal(t, "Ship parts", conv.Title)
	assert.Equal(t, "demo", conv.Project)
	assert.Equal(t, goal.ID, conv.GoalID)

	g.tick(t)

	require.Equal(t, 1, g.turns())
	assert.Equal(t, goal.ID, g.envs[0]["ONETT_GOAL"])
	assert.Equal(t, "orchestrator", g.envs[0]["ONETT_ROLE"])
	assert.Contains(t, g.prompts[0], "Ship parts")
	assert.Contains(t, g.prompts[0], "part 1 then part 2")
	assert.Contains(t, g.prompts[0], "No task has been queued")
	assert.Contains(t, g.prompts[0], "onett goal done "+goal.ID)

	tasks := g.tasksOf(t, goal.ID)
	require.Len(t, tasks, 1)
	assert.Equal(t, goal.ID, tasks[0].GoalID)

	got := g.goal(t, goal.ID)
	assert.Equal(t, 1, got.Turns)
	require.NotNil(t, got.LastTurnAt)
	assert.Equal(t, domain.GoalActive, got.Status, "a turn that queued work leaves the goal active")

	msgs, err := g.st.ListChatMessages(context.Background(), goal.ConversationID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, "system", msgs[0].Role, "the check-in is not an owner message")
	assert.Equal(t, "assistant", msgs[1].Role)

	after, err := g.st.GetConversation(context.Background(), goal.ConversationID)
	require.NoError(t, err)
	assert.False(t, after.Busy)
}

func TestGoalWaitsForUnsettledTasksThenTurns(t *testing.T) {
	g := newGoalRig(t)
	g.act = g.createsTask(t)
	goal := g.newGoal(t)
	g.tick(t)
	task := g.tasksOf(t, goal.ID)[0]
	require.Equal(t, 1, g.turns())

	for _, status := range []domain.TaskStatus{
		domain.TaskQueued, domain.TaskRunning, domain.TaskReview, domain.TaskAwaitingApproval,
		domain.TaskMerged, domain.TaskDeploying,
	} {
		ctSetTask(t, g.ctRig, task.ID, func(tk *domain.Task) { tk.Status = status })
		g.tick(t)
		assert.Equal(t, 1, g.turns(), "no turn while a goal task is %s", status)
	}

	ctSetTask(t, g.ctRig, task.ID, func(tk *domain.Task) { tk.Status = domain.TaskDone; tk.Summary = "shipped part 1" })
	g.tick(t)
	require.Equal(t, 2, g.turns(), "the settled task wakes the orchestrator")
	assert.Contains(t, g.prompts[1], "shipped part 1")
	assert.Contains(t, g.prompts[1], task.ID)
	assert.Equal(t, 2, g.goal(t, goal.ID).Turns)

	// The second turn queued another task, so the goal waits again.
	g.tick(t)
	assert.Equal(t, 2, g.turns())
}

func TestGoalNoTurnWhenNothingChanged(t *testing.T) {
	g := newGoalRig(t)
	goal := g.newGoal(t)
	ctx := context.Background()

	// A turn that happened after the last task change, with no owner message since.
	last := time.Now().Add(time.Hour).UTC()
	_, err := g.st.UpdateGoal(ctx, goal.ID, func(x *domain.Goal) error {
		x.Turns, x.LastTurnAt = 1, &last
		return nil
	})
	require.NoError(t, err)
	task := ctTask(t, g.e, "old")
	ctSetTask(t, g.ctRig, task.ID, func(tk *domain.Task) { tk.GoalID = goal.ID; tk.Status = domain.TaskDone })

	g.tick(t)
	assert.Equal(t, 0, g.turns())

	// The owner speaking in the goal conversation is a change.
	_, err = g.st.AppendChatMessage(ctx, domain.ChatMessage{
		ConversationID: goal.ConversationID, Role: "user", Text: "also do X", CreatedAt: last.Add(time.Minute),
	})
	require.NoError(t, err)
	g.act = g.createsTask(t)
	g.tick(t)
	assert.Equal(t, 1, g.turns())
}

func TestGoalNoProgressBlocksAndNotifies(t *testing.T) {
	g := newGoalRig(t)
	goal := g.newGoal(t) // the orchestrator does nothing

	g.tick(t)

	got := g.goal(t, goal.ID)
	assert.Equal(t, domain.GoalBlocked, got.Status)
	assert.Equal(t, "the orchestrator made no progress on its last turn", got.Summary)
	notes := g.notified()
	require.Len(t, notes, 1)
	assert.Equal(t, notify.PriorityHigh, notes[0].Priority)
	assert.Contains(t, notes[0].Title, "Ship parts")

	g.tick(t)
	assert.Equal(t, 1, g.turns(), "a blocked goal gets no more turns")
}

func TestGoalRetryCountsAsProgress(t *testing.T) {
	g := newGoalRig(t)
	g.act = g.createsTask(t)
	goal := g.newGoal(t)
	g.tick(t)
	task := g.tasksOf(t, goal.ID)[0]
	ctSetTask(t, g.ctRig, task.ID, func(tk *domain.Task) { tk.Status = domain.TaskBlocked; tk.BlockedReason = "needs a retry" })

	g.act = func(ctx context.Context, _ harness.Spec) {
		assert.NoError(t, g.e.RetryTask(ctx, task.ID, "cause fixed"))
	}
	g.tick(t)

	assert.Equal(t, domain.GoalActive, g.goal(t, goal.ID).Status)
	assert.Contains(t, g.prompts[1], "needs a retry")
}

func TestGoalMaxTurnsBlocks(t *testing.T) {
	g := newGoalRig(t)
	g.cfg.Limits.MaxGoalTurns = 1
	g.act = g.createsTask(t)
	goal := g.newGoal(t)

	g.tick(t)

	got := g.goal(t, goal.ID)
	assert.Equal(t, domain.GoalBlocked, got.Status)
	assert.Contains(t, got.Summary, "limits.max_goal_turns")
	require.Len(t, g.notified(), 1)
	assert.Equal(t, notify.PriorityHigh, g.notified()[0].Priority)
}

func TestGoalPausedGetsNoTurns(t *testing.T) {
	g := newGoalRig(t)
	g.act = g.createsTask(t)
	goal := g.newGoal(t)
	ctx := context.Background()

	_, err := g.e.SetGoalStatus(ctx, goal.ID, domain.GoalPaused, "")
	require.NoError(t, err)
	g.tick(t)
	assert.Equal(t, 0, g.turns(), "paused goal")

	_, err = g.e.SetGoalStatus(ctx, goal.ID, domain.GoalActive, "")
	require.NoError(t, err)
	require.NoError(t, g.e.SetProjectPaused(ctx, "demo", true))
	g.tick(t)
	assert.Equal(t, 0, g.turns(), "paused project")
	require.NoError(t, g.e.SetProjectPaused(ctx, "demo", false))

	require.NoError(t, g.e.SetPaused(ctx, true))
	g.tick(t)
	assert.Equal(t, 0, g.turns(), "paused factory")
	require.NoError(t, g.e.SetPaused(ctx, false))

	g.tick(t)
	assert.Equal(t, 1, g.turns())
}

func TestGoalDoneAndBlockNotify(t *testing.T) {
	g := newGoalRig(t)
	ctx := context.Background()
	events, unsub := g.bus.Subscribe()
	defer unsub()

	done := g.newGoal(t)
	_, err := g.e.SetGoalStatus(ctx, done.ID, domain.GoalDone, "  ")
	assert.True(t, errors.Is(err, api.ErrInvalid), "a summary is required: %v", err)

	got, err := g.e.SetGoalStatus(ctx, done.ID, domain.GoalDone, "all parts merged")
	require.NoError(t, err)
	assert.Equal(t, domain.GoalDone, got.Status)
	assert.Equal(t, "all parts merged", got.Summary)
	_, err = g.e.SetGoalStatus(ctx, done.ID, domain.GoalBlocked, "late")
	assert.True(t, errors.Is(err, api.ErrConflict), "a done goal cannot be blocked: %v", err)
	_, err = g.e.SetGoalStatus(ctx, done.ID, domain.GoalPaused, "")
	assert.True(t, errors.Is(err, api.ErrConflict), "a done goal cannot be paused: %v", err)

	blocked := g.newGoal(t)
	_, err = g.e.SetGoalStatus(ctx, blocked.ID, domain.GoalBlocked, "needs an API key")
	require.NoError(t, err)
	_, err = g.e.SetGoalStatus(ctx, blocked.ID, domain.GoalBlocked, "again")
	assert.True(t, errors.Is(err, api.ErrConflict), "already blocked: %v", err)
	resumed, err := g.e.SetGoalStatus(ctx, blocked.ID, domain.GoalActive, "")
	require.NoError(t, err)
	assert.Equal(t, domain.GoalActive, resumed.Status)

	notes := g.notified()
	require.Len(t, notes, 2)
	assert.Equal(t, notify.PriorityNormal, notes[0].Priority)
	assert.Equal(t, "all parts merged", notes[0].Body)
	assert.Equal(t, notify.PriorityHigh, notes[1].Priority)
	assert.Equal(t, "needs an API key", notes[1].Body)

	var updates int
	for len(events) > 0 {
		if (<-events).Type == domain.EvGoalUpdated {
			updates++
		}
	}
	assert.GreaterOrEqual(t, updates, 5, "creation and every status change emit goal.updated")
}

func TestGoalFinishedByTheOrchestratorIsNotBlocked(t *testing.T) {
	g := newGoalRig(t)
	goal := g.newGoal(t)
	g.act = func(ctx context.Context, spec harness.Spec) {
		_, err := g.e.SetGoalStatus(ctx, spec.Env["ONETT_GOAL"], domain.GoalDone, "nothing left to do")
		assert.NoError(t, err)
	}

	g.tick(t)

	got := g.goal(t, goal.ID)
	assert.Equal(t, domain.GoalDone, got.Status, "finishing is progress")
	assert.Len(t, g.notified(), 1)
}

func TestCreateTaskChecksGoal(t *testing.T) {
	g := newGoalRig(t)
	ctx := context.Background()
	_, err := g.e.CreateTask(ctx, domain.Task{Project: "demo", Kind: domain.KindChore, Title: "x", GoalID: "nope"})
	assert.True(t, errors.Is(err, api.ErrInvalid), "unknown goal: %v", err)

	goal := g.newGoal(t)
	task, err := g.e.CreateTask(ctx, domain.Task{Project: "demo", Kind: domain.KindChore, Title: "x", GoalID: goal.ID})
	require.NoError(t, err)
	assert.Equal(t, goal.ID, task.GoalID)

	_, err = g.e.CreateGoal(ctx, domain.Goal{Project: "nope", Title: "x"})
	assert.True(t, errors.Is(err, api.ErrInvalid), "unknown project: %v", err)
	_, err = g.e.CreateGoal(ctx, domain.Goal{Project: "demo", Title: " "})
	assert.True(t, errors.Is(err, api.ErrInvalid), "missing title: %v", err)
}

func TestGoalInstructionsOnlyInGoalConversations(t *testing.T) {
	cfg := ctNewRig(t).cfg
	plain := chatInstructions(cfg, domain.Conversation{Project: "demo"}, "BASE")
	assert.NotContains(t, plain, "runs a goal")
	goal := chatInstructions(cfg, domain.Conversation{Project: "demo", GoalID: "g1"}, "BASE")
	assert.Contains(t, goal, "BASE")
	assert.Contains(t, goal, "onett goal done g1 --summary")
	assert.Contains(t, goal, "onett goal block g1 --reason")
}

func storeGoalFilter(id string) store.TaskFilter { return store.TaskFilter{GoalID: id} }
