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

// A chat run moves from starting to running once its agent has started, like a task run does,
// so the Runs page does not show a live turn as "starting".
func TestChatRunIsRunningWhileAgentWorks(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	seen := make(chan domain.RunStatus, 1)
	e.d.HarnessFor = func(string, config.Harness) (harness.Harness, error) {
		return hfake.New(func(ctx context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
			// The fake starts working at once; the real agent takes a while, so give the
			// transition a moment and report the status it settles on.
			var status domain.RunStatus
			for range 200 {
				if run, err := e.d.Store.GetRun(ctx, spec.RunID); err == nil {
					status = run.Status
				}
				if status == domain.RunRunning {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			seen <- status
			return harness.Result{SessionID: "s", FinalText: "ok"}, nil
		}), nil
	}

	conv, err := r.st.CreateConversation(ctx, domain.Conversation{Project: "demo"})
	require.NoError(t, err)
	_, err = e.SendChat(ctx, conv.ID, "hello")
	require.NoError(t, err)
	ctWaitIdle(t, r, conv.ID)

	assert.Equal(t, domain.RunRunning, <-seen)
}
