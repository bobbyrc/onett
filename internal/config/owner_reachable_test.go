package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOwnerReachable(t *testing.T) {
	def, err := Parse(nil, "")
	require.NoError(t, err)
	assert.False(t, def.OwnerReachable(), "default config only has the log notifier")

	assert.False(t, (&Config{Notify: []Notifier{{Type: "log"}, {Type: "log"}}}).OwnerReachable())
	assert.True(t, (&Config{Notify: []Notifier{{Type: "log"}, {Type: "ntfy"}}}).OwnerReachable())
	assert.True(t, (&Config{Notify: []Notifier{{Type: "telegram"}}}).OwnerReachable())
	assert.True(t, (&Config{Notify: []Notifier{{Type: "log"}}, Channels: []Channel{{Type: "telegram"}}}).OwnerReachable())
}
