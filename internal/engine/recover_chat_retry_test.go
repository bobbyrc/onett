package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

// These tests cover an orchestrator chat turn that a restart interrupted when the startup
// recovery itself fails part-way and is finished by the next start.

// crcSeed stores what a killed daemon leaves behind in the middle of a chat turn: a busy
// orchestrator conversation whose run is still marked running. It makes the engine use a fake
// harness that sends each prompt it is given to the returned channel.
func crcSeed(t *testing.T, env *lpEnv) (domain.Conversation, domain.Run, <-chan string) {
	t.Helper()
	prompts := make(chan string, 4)
	env.e.d.HarnessFor = func(string, config.Harness) (harness.Harness, error) {
		return hfake.New(func(_ context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
			prompts <- spec.Prompt
			return harness.Result{SessionID: "s2", FinalText: "carried on"}, nil
		}), nil
	}
	// Turns started by recover run in tracked goroutines; wait for them before the store closes.
	t.Cleanup(env.e.wg.Wait)

	conv, err := env.st.CreateConversation(env.ctx, domain.Conversation{Title: "chat", Project: "demo"})
	require.NoError(t, err)
	run, err := env.st.CreateRun(env.ctx, domain.Run{
		Project: "demo", Role: "orchestrator", Harness: "fake",
		Status: domain.RunRunning, StartedAt: time.Now().Add(-time.Minute),
	})
	require.NoError(t, err)
	conv, err = env.st.UpdateConversation(env.ctx, conv.ID, func(c *domain.Conversation) error {
		c.Busy, c.ActiveRunID, c.SessionID = true, run.ID, "prev-session"
		return nil
	})
	require.NoError(t, err)
	return conv, run, prompts
}

// crcAssertRerun waits for the interrupted turn to be run again and for the conversation to be
// released afterwards.
func crcAssertRerun(t *testing.T, env *lpEnv, conv domain.Conversation, prompts <-chan string) {
	t.Helper()
	select {
	case p := <-prompts:
		assert.Contains(t, p, chatInterruptedNote)
	case <-time.After(10 * time.Second):
		got, err := env.st.GetConversation(env.ctx, conv.ID)
		require.NoError(t, err)
		t.Fatalf("the interrupted chat turn was not run again (conversation busy: %v, active run: %q)", got.Busy, got.ActiveRunID)
	}
	rtWait(t, 15*time.Second, "the re-run turn to finish", func() bool {
		got, err := env.st.GetConversation(env.ctx, conv.ID)
		return err == nil && !got.Busy
	})
}

// Control for the retry test below: a clean recovery runs the interrupted chat turn again.
func TestRecoverRunsAnInterruptedChatTurnAgain(t *testing.T) {
	env := cprEnv(t)
	conv, run, prompts := crcSeed(t, env)

	require.NoError(t, env.e.recover(env.ctx))

	crcAssertRerun(t, env, conv, prompts)
	got, err := env.st.GetRun(env.ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.RunFailed, got.Status)
}

// A recovery that fails after it ended the turn's run must not cost the turn: the next start
// still runs it again.
func TestRecoverRetryStillRunsAnInterruptedChatTurnAgain(t *testing.T) {
	env := cprEnv(t)
	conv, run, prompts := crcSeed(t, env)
	cs := cpWrap(env.e)
	cs.failOn("ListConversations", cpCall(1)) // after the run was ended, before the conversation is reached

	require.ErrorIs(t, env.e.recover(env.ctx), errCPInjected)
	got, err := env.st.GetRun(env.ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, domain.RunFailed, got.Status, "the failed start already ended the run")
	stuck, err := env.st.GetConversation(env.ctx, conv.ID)
	require.NoError(t, err)
	require.True(t, stuck.Busy, "the failed start did not reach the conversation")

	cs.heal()
	require.NoError(t, env.e.recover(env.ctx), "the next start finishes the recovery")
	crcAssertRerun(t, env, conv, prompts)
}
