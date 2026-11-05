package claude

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/harness"
)

// A Cancel that lands after a turn ended (its context is cancelled right after the result) must
// not kill the idle process: the next turn still reuses it.
func TestCancelAfterTurnEndedKeepsTheProcess(t *testing.T) {
	r := newStubRunner(t)
	conv := t.Name()
	t.Cleanup(harness.StopConversations)
	dir := t.TempDir()
	turn := func(prompt string) (harness.Session, harness.Result) {
		sess, err := r.Start(context.Background(), harness.Spec{
			Workdir: dir, Prompt: prompt, ConversationID: conv, KeepAlive: time.Minute,
		})
		require.NoError(t, err)
		for range sess.Events() {
		}
		res, err := sess.Wait()
		require.NoError(t, err)
		return sess, res
	}
	s1, first := turn("one")
	s1.Cancel() // stale
	s1.Cancel()
	time.Sleep(100 * time.Millisecond)
	_, second := turn("two")

	m1, m2 := pidRe.FindStringSubmatch(first.FinalText), pidRe.FindStringSubmatch(second.FinalText)
	require.NotNil(t, m1)
	require.NotNil(t, m2)
	assert.Equal(t, m1[1], m2[1], "the stale Cancel did not replace the process")
	assert.False(t, second.IsError)
}
