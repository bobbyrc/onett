package channel_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/channel"
	"github.com/bobbyrc/onett/internal/config"
)

func noopFactory(config.Channel, channel.Env) (channel.Starter, error) { return nil, nil }

func TestRegisterLookupAndTypes(t *testing.T) {
	channel.Register("zz-registry-b", channel.Registration{Factory: noopFactory, SkipApprovalsForNotifier: "x"})
	channel.Register("zz-registry-a", channel.Registration{Factory: noopFactory})

	r, ok := channel.Lookup("zz-registry-b")
	require.True(t, ok)
	assert.Equal(t, "x", r.SkipApprovalsForNotifier)
	_, ok = channel.Lookup("zz-registry-missing")
	assert.False(t, ok)

	types := channel.Types()
	assert.Contains(t, types, "telegram", "the host_test imports the telegram package")
	assert.IsIncreasing(t, types)
}

func TestRegisterPanicsOnBadInput(t *testing.T) {
	assert.Panics(t, func() { channel.Register("", channel.Registration{Factory: noopFactory}) })
	assert.Panics(t, func() { channel.Register("zz-registry-nofactory", channel.Registration{}) })
	channel.Register("zz-registry-dup", channel.Registration{Factory: noopFactory})
	assert.Panics(t, func() { channel.Register("zz-registry-dup", channel.Registration{Factory: noopFactory}) })
}
