package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
)

// The entries init writes are the ones the shared builders (and so the web UI) produce.
func TestInitConfigMatchesSharedEntries(t *testing.T) {
	for _, ch := range []string{"telegram", "discord"} {
		text := initialConfigWith("", "", "", notifySetup{channel: ch, userIDs: []string{"42", "7"}})
		cfg, err := config.Parse([]byte(text), "onett.yaml")
		require.NoError(t, err, ch)
		want, err := config.ChannelEntry(ch, []string{"42", "7"})
		require.NoError(t, err)
		require.Len(t, cfg.Channels, 1)
		assert.Equal(t, want, cfg.Channels[0], ch)
	}

	text := initialConfigWith("", "", "", notifySetup{ntfyTopic: "t-1", ntfyServer: "https://ntfy.example"})
	cfg, err := config.Parse([]byte(text), "onett.yaml")
	require.NoError(t, err)
	require.Len(t, cfg.Notify, 2) // log, then ntfy
	assert.Equal(t, config.NtfyEntry("t-1", "https://ntfy.example"), cfg.Notify[1])
}
