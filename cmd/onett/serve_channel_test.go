package main

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/channel"
	"github.com/bobbyrc/onett/internal/config"
)

// resolve runs channel.Resolve against the process environment.
func resolve(t *testing.T, cfg *config.Config) (int, map[string]bool, error) {
	t.Helper()
	skip, err := channel.Resolve(cfg.Channels, channel.Env{DataDir: cfg.Server.DataDir, Getenv: os.Getenv, Logger: slog.Default()})
	return len(cfg.Channels), skip, err
}

func TestResolveChannelsTelegramTokenFromEnv(t *testing.T) {
	cfg := &config.Config{
		Server: config.Server{DataDir: "/d"},
		Channels: []config.Channel{{Type: "telegram", Options: map[string]any{
			"bot_token_env": "TEST_TG_TOKEN", "allowed_user_ids": []any{5},
		}}},
	}
	t.Setenv("TEST_TG_TOKEN", "")
	_, _, err := resolve(t, cfg)
	require.Error(t, err)
	assert.Equal(t, `channel telegram: environment variable "TEST_TG_TOKEN" (bot_token_env) is empty`, err.Error())

	t.Setenv("TEST_TG_TOKEN", "sekret")
	_, skip, err := resolve(t, cfg)
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"telegram": true}, skip)

	_, skip, err = resolve(t, &config.Config{})
	require.NoError(t, err)
	assert.Empty(t, skip)
}

func TestResolveChannelsDiscordTokenFromEnv(t *testing.T) {
	cfg := &config.Config{
		Server: config.Server{DataDir: "/d"},
		Channels: []config.Channel{{Type: "discord", Options: map[string]any{
			"bot_token_env": "TEST_DC_TOKEN", "allowed_user_ids": []any{"123456789012345678"},
		}}},
	}
	t.Setenv("TEST_DC_TOKEN", "")
	_, _, err := resolve(t, cfg)
	require.Error(t, err)
	assert.Equal(t, `channel discord: environment variable "TEST_DC_TOKEN" (bot_token_env) is empty`, err.Error())

	t.Setenv("TEST_DC_TOKEN", "sekret")
	_, skip, err := resolve(t, cfg)
	require.NoError(t, err)
	assert.Empty(t, skip, "discord does not displace a notifier")
}

func TestResolveChannelsRejectsUnknownType(t *testing.T) {
	_, _, err := resolve(t, &config.Config{Channels: []config.Channel{{Type: "carrier-pigeon"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "carrier-pigeon")
	assert.Contains(t, err.Error(), "discord, telegram")
}

func TestNotifyConfigsSkipApprovalsForNamedTypes(t *testing.T) {
	list := []config.Notifier{
		{Type: "telegram", Options: map[string]any{"chat_id": "1"}},
		{Type: "log"},
	}
	out := notifyConfigs(list, map[string]bool{"telegram": true})
	assert.Equal(t, true, out[0].Options["skip_approvals"])
	assert.NotContains(t, list[0].Options, "skip_approvals", "the config must not be mutated")
	assert.Nil(t, out[1].Options)

	out = notifyConfigs(list, nil)
	assert.NotContains(t, out[0].Options, "skip_approvals")
}
