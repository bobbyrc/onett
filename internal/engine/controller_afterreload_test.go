package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
)

func TestReloadConfigRunsAfterReloadAndReportsItsError(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(func(string) (*config.Config, error) { return r.cfg, nil })

	var got *config.Config
	e.d.AfterReload = func(_ context.Context, c *config.Config) error { got = c; return nil }
	require.NoError(t, e.ReloadConfig(ctx))
	assert.Same(t, r.cfg, got, "AfterReload gets the new config")

	e.d.AfterReload = func(context.Context, *config.Config) error { return errors.New("notifiers not reloaded: bad token") }
	err := e.ReloadConfig(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notifiers not reloaded")
	assert.NotErrorIs(t, err, errors.ErrUnsupported)
}
