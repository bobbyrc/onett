package engine

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
	"github.com/bobbyrc/onett/internal/scm"
	"github.com/bobbyrc/onett/internal/store"
	"github.com/bobbyrc/onett/internal/workspace"
)

// settingsEngine builds an engine whose harness records the Spec of every turn.
func settingsEngine(t *testing.T) (*Engine, *ctRig, func() []harness.Spec) {
	t.Helper()
	r := ctNewRig(t)
	var mu sync.Mutex
	var specs []harness.Spec
	h := hfake.New(func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		mu.Lock()
		specs = append(specs, spec)
		mu.Unlock()
		return hfake.DefaultScript(ctx, spec, emit)
	})
	e, err := New(Deps{
		Config: r.cfg, Store: r.st, Bus: r.bus, Workspaces: workspace.New(t.TempDir()),
		SCMFor:     func(*config.Project) (scm.SCM, error) { return r.fk, nil },
		HarnessFor: func(string, config.Harness) (harness.Harness, error) { return h, nil },
	})
	require.NoError(t, err)
	return e, r, func() []harness.Spec {
		mu.Lock()
		defer mu.Unlock()
		return append([]harness.Spec(nil), specs...)
	}
}

func TestConversationSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	e, r, _ := settingsEngine(t)
	conv, err := r.st.CreateConversation(ctx, domain.Conversation{Project: "demo"})
	require.NoError(t, err)

	v, err := e.ConversationSettings(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ConversationSettings{}, v.Settings, "a new conversation uses the role's defaults")
	assert.NotEmpty(t, v.Choices.Efforts)

	want := domain.ConversationSettings{Model: "big", Effort: "high", PermissionMode: "safe"}
	v, err = e.UpdateConversationSettings(ctx, conv.ID, want, false)
	require.NoError(t, err)
	assert.Equal(t, want, v.Settings)

	got, err := r.st.GetConversation(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, want, got.Settings, "stored")

	v, err = e.UpdateConversationSettings(ctx, conv.ID, domain.ConversationSettings{}, false)
	require.NoError(t, err)
	assert.Equal(t, domain.ConversationSettings{}, v.Settings, "an empty update resets to the role default")
}

func TestConversationSettingsValidation(t *testing.T) {
	ctx := context.Background()
	e, r, _ := settingsEngine(t)
	conv, err := r.st.CreateConversation(ctx, domain.Conversation{Project: "demo"})
	require.NoError(t, err)

	for name, in := range map[string]domain.ConversationSettings{
		"model":  {Model: "nope"},
		"effort": {Effort: "extreme"},
		"mode":   {PermissionMode: "yolo"},
	} {
		_, err := e.UpdateConversationSettings(ctx, conv.ID, in, false)
		assert.ErrorIs(t, err, api.ErrInvalid, name)
	}
}

func TestConversationSettingsDangerousModeNeedsConfirmation(t *testing.T) {
	ctx := context.Background()
	e, r, _ := settingsEngine(t)
	conv, err := r.st.CreateConversation(ctx, domain.Conversation{Project: "demo"})
	require.NoError(t, err)

	bypass := domain.ConversationSettings{PermissionMode: "bypass"}
	_, err = e.UpdateConversationSettings(ctx, conv.ID, bypass, false)
	require.ErrorIs(t, err, api.ErrInvalid)
	assert.Contains(t, err.Error(), "skips safety checks")

	_, err = e.UpdateConversationSettings(ctx, conv.ID, bypass, true)
	require.NoError(t, err)

	// Already set: changing another field does not ask again.
	_, err = e.UpdateConversationSettings(ctx, conv.ID, domain.ConversationSettings{PermissionMode: "bypass", Effort: "low"}, false)
	require.NoError(t, err)
}

func TestConversationSettingsApplyFromNextTurn(t *testing.T) {
	ctx := context.Background()
	e, r, specs := settingsEngine(t)
	conv, err := r.st.CreateConversation(ctx, domain.Conversation{Project: "demo"})
	require.NoError(t, err)

	_, err = e.SendChat(ctx, conv.ID, "first")
	require.NoError(t, err)
	ctWaitIdle(t, r, conv.ID)

	_, err = e.UpdateConversationSettings(ctx, conv.ID, domain.ConversationSettings{Model: "big", Effort: "high", PermissionMode: "safe"}, false)
	require.NoError(t, err)

	_, err = e.SendChat(ctx, conv.ID, "second")
	require.NoError(t, err)
	ctWaitIdle(t, r, conv.ID)

	got := specs()
	require.Len(t, got, 2)
	assert.Empty(t, got[0].Effort, "the first turn had no settings")
	assert.Empty(t, got[0].PermissionMode)
	assert.Equal(t, "big", got[1].Model)
	assert.Equal(t, "high", got[1].Effort)
	assert.Equal(t, "safe", got[1].PermissionMode)

	runs, err := r.st.ListRuns(ctx, storeRunFilter())
	require.NoError(t, err)
	var recorded int
	for _, run := range runs {
		if run.Effort == "high" && run.PermissionMode == "safe" && run.Model == "big" {
			recorded++
		}
	}
	assert.Equal(t, 1, recorded, "the run records the settings it used")
}

func storeRunFilter() store.RunFilter { return store.RunFilter{Limit: 100} }
