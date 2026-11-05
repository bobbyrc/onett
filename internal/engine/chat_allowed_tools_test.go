package engine

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

func TestChatTurnPreApprovesOnettCLI(t *testing.T) {
	var mu sync.Mutex
	var got []string
	h := hfake.NewConversational(func(_ context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
		mu.Lock()
		got = spec.AllowedTools
		mu.Unlock()
		return harness.Result{SessionID: "s", FinalText: "ok"}, nil
	})
	r, e, convID := cqRig(t, h)

	llSend(t, r, e, convID, "cancel the stuck task")

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"Bash(onett:*)"}, got, "only the onett command is pre-approved")
	assert.NotContains(t, got, "Bash", "no wildcard Bash")
}
