package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
)

func TestTelegramChannelOptionsResolvesTokenFromEnv(t *testing.T) {
	cfg := &config.Config{
		Server: config.Server{DataDir: "/d"},
		Channels: []config.Channel{{Type: "telegram", Options: map[string]any{
			"bot_token_env": "TEST_TG_TOKEN", "allowed_user_ids": []any{5},
		}}},
	}
	t.Setenv("TEST_TG_TOKEN", "")
	_, err := telegramChannelOptions(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TEST_TG_TOKEN")

	t.Setenv("TEST_TG_TOKEN", "sekret")
	o, err := telegramChannelOptions(cfg)
	require.NoError(t, err)
	assert.Equal(t, "sekret", o.Token)
	assert.Equal(t, []int64{5}, o.AllowedUserIDs)
	assert.Equal(t, "/d/channels/telegram.json", o.StateFile)

	o, err = telegramChannelOptions(&config.Config{})
	require.NoError(t, err)
	assert.Nil(t, o)
}

func TestNotifyConfigsSkipApprovalsOnTelegramOnly(t *testing.T) {
	list := []config.Notifier{
		{Type: "telegram", Options: map[string]any{"chat_id": "1"}},
		{Type: "log"},
	}
	out := notifyConfigs(list, true)
	assert.Equal(t, true, out[0].Options["skip_approvals"])
	assert.NotContains(t, list[0].Options, "skip_approvals", "the config must not be mutated")
	assert.Nil(t, out[1].Options)

	out = notifyConfigs(list, false)
	assert.NotContains(t, out[0].Options, "skip_approvals")
}

func TestDiscordChannelOptionsResolvesTokenFromEnv(t *testing.T) {
	cfg := &config.Config{
		Server: config.Server{DataDir: "/d"},
		Channels: []config.Channel{{Type: "discord", Options: map[string]any{
			"bot_token_env": "TEST_DC_TOKEN", "allowed_user_ids": []any{"123456789012345678"},
		}}},
	}
	t.Setenv("TEST_DC_TOKEN", "")
	_, err := discordChannelOptions(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TEST_DC_TOKEN")

	t.Setenv("TEST_DC_TOKEN", "sekret")
	o, err := discordChannelOptions(cfg)
	require.NoError(t, err)
	assert.Equal(t, "sekret", o.Token)
	assert.Equal(t, []string{"123456789012345678"}, o.AllowedUserIDs)
	assert.Equal(t, "/d/channels/discord.json", o.StateFile)

	o, err = discordChannelOptions(&config.Config{})
	require.NoError(t, err)
	assert.Nil(t, o)
}
