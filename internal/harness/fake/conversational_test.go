package fake

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/harness"
)

// convScript answers at once, except for the prompt "block", which waits until it is cancelled.
func convScript(ctx context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
	if spec.Prompt == "block" {
		<-ctx.Done()
	}
	return harness.Result{FinalText: "ok"}, nil
}

// convTurn runs one turn of the conversation "conv" to its end and returns its session.
func convTurn(t *testing.T, ctx context.Context, c *Conversational, keepAlive time.Duration) harness.Session {
	t.Helper()
	sess, err := c.Start(ctx, harness.Spec{ConversationID: "conv", KeepAlive: keepAlive, Prompt: "hi"})
	require.NoError(t, err)
	for range sess.Events() {
	}
	_, err = sess.Wait()
	require.NoError(t, err)
	return sess
}

// The engine cancels a turn's context, and may cancel the session, right after the turn's result.
// That is not a cancelled turn: the process stays for the next message, as a real one does.
func TestConversationalKeepsTheProcessWhenCancelledAfterTheTurnEnded(t *testing.T) {
	c := NewConversational(convScript)
	ctx, cancel := context.WithCancel(context.Background())
	sess := convTurn(t, ctx, c, time.Minute)
	cancel()
	sess.Cancel()

	convTurn(t, context.Background(), c, time.Minute)
	turns := c.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, turns[0].Process, turns[1].Process, "the same process answered both turns")
	assert.Equal(t, 1, c.Processes())
}

// A context cancelled right after the result must not stop the idle timeout from ending the process.
func TestConversationalIdleTimeoutEndsTheProcessAfterALateContextCancel(t *testing.T) {
	c := NewConversational(convScript)
	ctx, cancel := context.WithCancel(context.Background())
	convTurn(t, ctx, c, 20*time.Millisecond)
	cancel()

	require.Eventually(t, func() bool { return c.Live() == 0 }, 10*time.Second, 5*time.Millisecond,
		"the idle process never ended")
	convTurn(t, context.Background(), c, 20*time.Millisecond)
	assert.Equal(t, 2, c.Processes(), "the next turn started a new process")
}

// A turn cancelled before it ended kills the process.
func TestConversationalCancelDuringATurnEndsTheProcess(t *testing.T) {
	c := NewConversational(convScript)
	sess, err := c.Start(context.Background(), harness.Spec{ConversationID: "conv", KeepAlive: time.Minute, Prompt: "block"})
	require.NoError(t, err)
	require.Equal(t, 1, c.Live())
	sess.Cancel()
	_, _ = sess.Wait()
	assert.Equal(t, 0, c.Live(), "the cancelled turn's process is gone")

	convTurn(t, context.Background(), c, time.Minute)
	assert.Equal(t, 2, c.Processes(), "the next turn started a new process")
}

// A turn whose context ends midway kills the process too, and it is gone by the time the turn's
// session is done, so the next turn cannot land on it.
func TestConversationalContextCancelDuringATurnEndsTheProcess(t *testing.T) {
	c := NewConversational(convScript)
	ctx, cancel := context.WithCancel(context.Background())
	sess, err := c.Start(ctx, harness.Spec{ConversationID: "conv", KeepAlive: time.Minute, Prompt: "block"})
	require.NoError(t, err)
	require.Equal(t, 1, c.Live())
	cancel()
	_, _ = sess.Wait()
	assert.Equal(t, 0, c.Live(), "the cancelled turn's process is gone once the turn is done")

	convTurn(t, context.Background(), c, time.Minute)
	assert.Equal(t, 2, c.Processes(), "the next turn started a new process")
}
