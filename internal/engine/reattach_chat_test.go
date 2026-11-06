package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// These tests restart the engine in the middle of an orchestrator turn (a chat reply or a goal
// check-in). They reuse reattachFixture: the agent is the same shell script, which also records
// the session it was asked to resume and the prompt it was given.
const chatAgent = `#!/bin/sh
{ echo "resume=$ONETT_RESUME_SESSION"; cat; echo "@@END@@"; } >> "$SEEN_FILE"
echo '{"type":"text","text":"line one","session_id":"s1"}'
echo '{"type":"text","text":"line two"}'
while [ ! -e "$GATE_FILE" ]; do sleep 0.05; done
echo '{"type":"text","text":"line three"}'
echo '{"type":"result","session_id":"s1","text":"all done"}'
`

type chatFixture struct {
	*reattachFixture
	seen string
}

func newChatFixture(t *testing.T) *chatFixture {
	t.Helper()
	f := newReattachFixture(t, time.Hour, time.Hour)
	root := filepath.Dir(f.dataDir)
	require.NoError(t, os.WriteFile(filepath.Join(root, "agent.sh"), []byte(chatAgent), 0o755))
	c := &chatFixture{reattachFixture: f, seen: filepath.Join(root, "seen")}
	// An orchestrator turn gets no project environment, only the harness's.
	h := f.cfg.Harness["plug"]
	h.Env = map[string]string{"GATE_FILE": f.gate, "SEEN_FILE": c.seen}
	f.cfg.Harness["plug"] = h
	return c
}

// started waits until the engine has finished recovering and started its loops. A test that
// touches a conversation before then would race with recover's sweep of busy conversations.
func (f *chatFixture) started(e *Engine) {
	f.t.Helper()
	rtWait(f.t, 15*time.Second, "the engine to start its loops", func() bool {
		e.kickMu.Lock()
		defer e.kickMu.Unlock()
		return len(e.kicks) > 0
	})
}

func (f *chatFixture) conversation(id string) domain.Conversation {
	f.t.Helper()
	c, err := f.st.GetConversation(context.Background(), id)
	require.NoError(f.t, err)
	return c
}

func (f *chatFixture) goal(id string) domain.Goal {
	f.t.Helper()
	g, err := f.st.GetGoal(context.Background(), id)
	require.NoError(f.t, err)
	return g
}

func (f *chatFixture) assistantTexts(convID string) []string {
	f.t.Helper()
	msgs, err := f.st.ListChatMessages(context.Background(), convID)
	require.NoError(f.t, err)
	var out []string
	for _, m := range msgs {
		if m.Role == "assistant" {
			out = append(out, m.Text)
		}
	}
	return out
}

// turnRun waits for the conversation's orchestrator run to have stored its first two lines and
// the stream offset past them.
func (f *chatFixture) turnRun(convID string) domain.Run {
	f.t.Helper()
	var run domain.Run
	rtWait(f.t, 15*time.Second, "the turn's first lines and their offset to be stored", func() bool {
		c := f.conversation(convID)
		if !c.Busy || c.ActiveRunID == "" {
			return false
		}
		r, err := f.st.GetRun(context.Background(), c.ActiveRunID)
		if err != nil || len(f.logTexts(r.ID)) != 2 {
			return false
		}
		run = r
		return r.StreamOffset > 0 && r.StreamOffset == f.streamSize(r.ID)
	})
	return run
}

func (f *chatFixture) waitIdle(convID string) {
	f.t.Helper()
	rtWait(f.t, 30*time.Second, "the conversation to be released", func() bool { return !f.conversation(convID).Busy })
}

func (f *chatFixture) openGate() {
	f.t.Helper()
	require.NoError(f.t, os.WriteFile(f.gate, nil, 0o644))
}

// killShim stops the run's shim and agent, as a reboot would.
func (f *chatFixture) killShim(runID string) {
	f.t.Helper()
	st := f.shimStatus(runID)
	require.NoError(f.t, syscall.Kill(-st.AgentPGID, syscall.SIGKILL))
	require.NoError(f.t, syscall.Kill(st.ShimPID, syscall.SIGKILL))
	rtWait(f.t, 15*time.Second, "shim to die", func() bool { return syscall.Kill(st.ShimPID, 0) == syscall.ESRCH })
}

// seenRuns returns the "resume=...\n<prompt>" record of each agent process started.
func (f *chatFixture) seenRuns() []string {
	f.t.Helper()
	b, err := os.ReadFile(f.seen)
	require.NoError(f.t, err)
	parts := strings.Split(string(b), "@@END@@")
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parkGoalTask adds a goal task that waits for a person, so the goal loop starts no further turn
// while the test looks at the goal. Its UpdatedAt is later than the turn's start.
func (f *chatFixture) parkGoalTask(g domain.Goal) {
	f.t.Helper()
	_, err := f.st.CreateTask(context.Background(), domain.Task{
		Project: g.Project, Title: "parked", Kind: domain.KindFeature, GoalID: g.ID,
		Status: domain.TaskAwaitingApproval,
	})
	require.NoError(f.t, err)
}

func TestChatTurnSurvivesRestart(t *testing.T) {
	f := newChatFixture(t)
	e1, stop1 := f.engine(time.Now)
	f.started(e1)
	conv, err := f.st.CreateConversation(context.Background(), domain.Conversation{Title: "chat", Project: "demo"})
	require.NoError(t, err)
	_, err = e1.SendChat(context.Background(), conv.ID, "hello")
	require.NoError(t, err)
	run := f.turnRun(conv.ID)

	stop1()
	// The turn is still in flight: nothing stored as a reply, the conversation still busy.
	c := f.conversation(conv.ID)
	assert.True(t, c.Busy)
	assert.Equal(t, run.ID, c.ActiveRunID)
	assert.Empty(t, f.assistantTexts(conv.ID))
	assert.Equal(t, domain.RunRunning, mustRun(t, f, run.ID).Status) // not touched by the shutdown
	require.NoError(t, syscall.Kill(f.shimStatus(run.ID).AgentPID, 0), "the agent outlived the daemon")

	f.openGate()
	rtWait(t, 15*time.Second, "the agent to exit", func() bool { return f.shimStatus(run.ID).Exited })
	_, _ = f.engine(time.Now)
	f.waitIdle(conv.ID)

	assert.Equal(t, []string{"all done"}, f.assistantTexts(conv.ID), "one assistant message, not one per replay")
	c = f.conversation(conv.ID)
	assert.Equal(t, "s1", c.SessionID)
	assert.Empty(t, c.ActiveRunID)
	assert.Equal(t, domain.RunSucceeded, mustRun(t, f, run.ID).Status)
	assert.Equal(t, []string{"line one", "line two", "line three"}, f.logTexts(run.ID))
	assert.Len(t, f.seenRuns(), 1, "the turn was not run a second time")
	f.waitShimGone(run.ID)
}

func TestGoalTurnSurvivesRestart(t *testing.T) {
	for _, tc := range []struct {
		name      string
		progress  bool
		wantState domain.GoalStatus
	}{
		// A turn that queued nothing, after one that did not either, blocks the goal: judged on
		// the real result of the turn, after the restart.
		{"no progress blocks", false, domain.GoalBlocked},
		{"progress keeps it active", true, domain.GoalActive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newChatFixture(t)
			e1, stop1 := f.engine(time.Now)
			f.started(e1)
			g, err := e1.CreateGoal(context.Background(), domain.Goal{Project: "demo", Title: "goal", Body: "do it"})
			require.NoError(t, err)
			run := f.turnRun(g.ConversationID)
			assert.True(t, run.GoalTurn)
			stop1()

			g = f.goal(g.ID)
			require.Equal(t, 1, g.Turns)
			require.Equal(t, domain.GoalActive, g.Status)
			_, err = f.st.UpdateGoal(context.Background(), g.ID, func(g *domain.Goal) error {
				g.StalledTurns = 1 // the last turn made no progress either
				return nil
			})
			require.NoError(t, err)
			if tc.progress {
				f.parkGoalTask(g)
			}

			f.openGate()
			rtWait(t, 15*time.Second, "the agent to exit", func() bool { return f.shimStatus(run.ID).Exited })
			_, _ = f.engine(time.Now)
			f.waitIdle(g.ConversationID)

			rtWait(t, 15*time.Second, "the goal guard to run", func() bool {
				return f.goal(g.ID).Status == tc.wantState || (tc.progress && f.goal(g.ID).StalledTurns == 0)
			})
			got := f.goal(g.ID)
			assert.Equal(t, tc.wantState, got.Status)
			assert.Equal(t, 1, got.Turns, "the turn counts once")
			assert.Equal(t, []string{"all done"}, f.assistantTexts(g.ConversationID))
			assert.Len(t, f.seenRuns(), 1)
			f.waitShimGone(run.ID)
		})
	}
}

func TestInterruptedChatTurnIsRunAgain(t *testing.T) {
	f := newChatFixture(t)
	e1, stop1 := f.engine(time.Now)
	f.started(e1)
	conv, err := f.st.CreateConversation(context.Background(), domain.Conversation{Title: "chat", Project: "demo"})
	require.NoError(t, err)
	_, err = f.st.UpdateConversation(context.Background(), conv.ID, func(c *domain.Conversation) error {
		c.SessionID = "prev-session"
		return nil
	})
	require.NoError(t, err)
	_, err = e1.SendChat(context.Background(), conv.ID, "hello")
	require.NoError(t, err)
	run := f.turnRun(conv.ID)
	stop1()
	f.killShim(run.ID)

	f.openGate()
	_, _ = f.engine(time.Now)
	f.waitIdle(conv.ID)

	assert.Equal(t, domain.RunFailed, mustRun(t, f, run.ID).Status)
	assert.Equal(t, []string{"all done"}, f.assistantTexts(conv.ID))
	seen := f.seenRuns()
	require.Len(t, seen, 2)
	assert.Contains(t, seen[0], "hello")
	assert.Contains(t, seen[1], "resume=prev-session")
	assert.Contains(t, seen[1], chatInterruptedNote)
}

func TestInterruptedGoalTurnRerunIsCountedOnceAndJudged(t *testing.T) {
	f := newChatFixture(t)
	e1, stop1 := f.engine(time.Now)
	f.started(e1)
	g, err := e1.CreateGoal(context.Background(), domain.Goal{Project: "demo", Title: "goal", Body: "do it"})
	require.NoError(t, err)
	run := f.turnRun(g.ConversationID)
	stop1()
	require.Equal(t, 1, f.goal(g.ID).Turns)
	_, err = f.st.UpdateGoal(context.Background(), g.ID, func(g *domain.Goal) error {
		g.StalledTurns = 1 // the previous turn made no progress either
		return nil
	})
	require.NoError(t, err)
	f.parkGoalTask(f.goal(g.ID))
	f.killShim(run.ID)

	f.openGate()
	_, _ = f.engine(time.Now)
	f.waitIdle(g.ConversationID)

	// The re-run queued nothing, so the normal guard blocks the goal (it would otherwise sit idle).
	rtWait(t, 15*time.Second, "the goal guard to judge the re-run", func() bool {
		return f.goal(g.ID).Status == domain.GoalBlocked
	})
	got := f.goal(g.ID)
	assert.Equal(t, 1, got.Turns, "the interrupted turn and its re-run count once")
	assert.Contains(t, f.seenRuns()[1], chatInterruptedNote)
	assert.Equal(t, []string{"all done"}, f.assistantTexts(g.ConversationID))
	runs, err := f.st.ListRuns(context.Background(), store.RunFilter{Project: "demo", Statuses: []domain.RunStatus{domain.RunStarting, domain.RunRunning}})
	require.NoError(t, err)
	assert.Empty(t, runs)
}

// A conversation left busy after its turn ended (a crash between storing the reply and clearing
// the flag) is only released: its run is over, so nothing is run again.
func TestFinishedTurnLeftBusyIsNotRunAgain(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	conv, err := f.st.CreateConversation(ctx, domain.Conversation{Title: "chat", Project: "demo"})
	require.NoError(t, err)
	now := time.Now().UTC()
	run, err := f.st.CreateRun(ctx, domain.Run{
		Project: "demo", Role: "orchestrator", Status: domain.RunSucceeded, StartedAt: now, EndedAt: &now,
	})
	require.NoError(t, err)
	_, err = f.st.UpdateConversation(ctx, conv.ID, func(c *domain.Conversation) error {
		c.Busy, c.ActiveRunID = true, run.ID
		return nil
	})
	require.NoError(t, err)

	e, _ := f.engine(time.Now)
	f.started(e)
	f.waitIdle(conv.ID)

	assert.Empty(t, f.conversation(conv.ID).ActiveRunID)
	_, err = os.Stat(f.seen)
	assert.True(t, os.IsNotExist(err), "no agent was started")
	assert.Empty(t, f.assistantTexts(conv.ID))
}

func mustRun(t *testing.T, f *chatFixture, id string) domain.Run {
	t.Helper()
	r, err := f.st.GetRun(context.Background(), id)
	require.NoError(t, err)
	return r
}
