package claude

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/harness"
)

// A new model or permission mode goes to the running process as control requests ahead of the
// message, so the process (and its prompt cache) is kept; a new effort replaces it.
func TestModelAndModeChangeInPlace(t *testing.T) {
	r := newStubRunner(t)
	conv := t.Name()
	t.Cleanup(harness.StopConversations)
	dir := t.TempDir()
	turn := func(model, mode, effort, prompt string) harness.Result {
		sess, err := r.Start(context.Background(), harness.Spec{
			Workdir: dir, Prompt: prompt, ConversationID: conv, KeepAlive: time.Minute,
			Model: model, PermissionMode: mode, Effort: effort,
		})
		require.NoError(t, err)
		for range sess.Events() {
		}
		res, err := sess.Wait()
		require.NoError(t, err)
		return res
	}
	first := turn("opus", "auto", "", "one")
	second := turn("sonnet", "plan", "", "two")
	third := turn("sonnet", "plan", "", "three")
	fourth := turn("sonnet", "plan", "max", "four")

	pid := func(res harness.Result) string {
		m := pidRe.FindStringSubmatch(res.FinalText)
		require.NotNil(t, m, res.FinalText)
		return m[1]
	}
	assert.Equal(t, pid(first), pid(second), "a model and mode change keeps the process")
	assert.Contains(t, second.FinalText, "ctl=set_model=sonnet;set_permission_mode=plan; ")
	assert.Contains(t, second.FinalText, "text=two")
	assert.Contains(t, third.FinalText, "ctl=set_model=sonnet;set_permission_mode=plan; text=three", "an unchanged turn sends no request")
	assert.NotEqual(t, pid(third), pid(fourth), "a new effort needs a new process")
}

func TestRetuneLines(t *testing.T) {
	live := func(model, mode string) string {
		return liveSettings(harness.Config{}, harness.Spec{Model: model, PermissionMode: mode})
	}
	b, ok := retuneLines(live("opus", "auto"), live("opus", "auto"))
	assert.True(t, ok)
	assert.Empty(t, b)
	b, ok = retuneLines(live("", "auto"), live("haiku", "auto"))
	assert.True(t, ok)
	assert.JSONEq(t, `{"type":"control_request","request_id":"onett-set_model","request":{"subtype":"set_model","model":"haiku"}}`, string(b))
	_, ok = retuneLines(live("haiku", "auto"), live("", "auto"))
	assert.False(t, ok, "there is no request that goes back to the default model")
}
