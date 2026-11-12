package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
	"github.com/bobbyrc/onett/internal/store"
)

// llHarness is a conversational fake that answers every turn with a session ID.
func llHarness() *hfake.Conversational {
	return hfake.NewConversational(func(_ context.Context, _ harness.Spec, _ func(harness.Event)) (harness.Result, error) {
		return harness.Result{SessionID: "sess-1", FinalText: "ok"}, nil
	})
}

// llSend sends text and waits for the turn to finish.
func llSend(t *testing.T, r *ctRig, e *Engine, convID, text string) {
	t.Helper()
	_, err := e.SendChat(context.Background(), convID, text)
	require.NoError(t, err)
	ctWaitIdle(t, r, convID)
}

func TestTwoMessagesToOneConversationReuseOneAgentProcess(t *testing.T) {
	h := llHarness()
	r, e, convID := cqRig(t, h)

	llSend(t, r, e, convID, "first")
	llSend(t, r, e, convID, "second")

	turns := h.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, 1, h.Processes(), "both messages went to a single agent process")
	assert.Equal(t, turns[0].Process, turns[1].Process)
	assert.Equal(t, convID, turns[1].ConversationID)
	assert.Equal(t, "sess-1", turns[1].Resume, "the session is still passed, for a process that has to start")

	// Each turn is still its own run.
	runs, err := r.st.ListRuns(context.Background(), store.RunFilter{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, runs, 2)
}

func TestIdleTimeoutMakesTheNextMessageResumeInANewProcess(t *testing.T) {
	h := llHarness()
	r, e, convID := cqRig(t, h)
	r.cfg.Chat.SessionIdleTimeout.Duration = 50 * time.Millisecond

	llSend(t, r, e, convID, "first")
	// Wait for the idle timeout to end the process, not for a fixed time: on a loaded machine the
	// timer fires late.
	rtWait(t, 10*time.Second, "the idle process to end", func() bool { return h.Live() == 0 })
	llSend(t, r, e, convID, "second")

	turns := h.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, 2, h.Processes(), "the idle process was gone, so a new one started")
	assert.NotEqual(t, turns[0].Process, turns[1].Process)
	assert.Equal(t, "", turns[0].Resume)
	assert.Equal(t, "sess-1", turns[1].Resume, "the new process resumes the conversation's session")
}

func TestNegativeIdleTimeoutKeepsOneProcessPerMessage(t *testing.T) {
	h := llHarness()
	r, e, convID := cqRig(t, h)
	r.cfg.Chat.SessionIdleTimeout.Duration = -time.Second

	llSend(t, r, e, convID, "first")
	llSend(t, r, e, convID, "second")

	for _, turn := range h.Turns() {
		assert.Empty(t, turn.ConversationID, "no long-lived process was asked for")
	}
	assert.Equal(t, 0, h.Processes())
}
