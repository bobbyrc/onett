package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestPauseReasonPersistsAndResumeClearsIt(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	assert.Nil(t, e.PauseInfo())

	require.NoError(t, e.PauseWith(ctx, domain.PauseInfo{Reason: domain.PauseOwner, By: "api token"}))
	got := e.PauseInfo()
	require.NotNil(t, got)
	assert.Equal(t, domain.PauseOwner, got.Reason)
	assert.False(t, got.Since.IsZero())

	restarted := r.engine(nil)
	require.NoError(t, restarted.recover(ctx))
	got = restarted.PauseInfo()
	require.NotNil(t, got)
	assert.Equal(t, "api token", got.By)

	require.NoError(t, restarted.SetPaused(ctx, false))
	assert.Nil(t, restarted.PauseInfo())
	again := r.engine(nil)
	require.NoError(t, again.recover(ctx))
	assert.Nil(t, again.PauseInfo())
}

func TestPlainPauseIsOwnerAndUnknownReasonRefused(t *testing.T) {
	ctx := context.Background()
	e := ctNewRig(t).engine(nil)
	require.NoError(t, e.SetPaused(ctx, true))
	assert.Equal(t, domain.PauseOwner, e.PauseInfo().Reason)
	require.Error(t, e.PauseWith(ctx, domain.PauseInfo{Reason: "self-update"}))
}
