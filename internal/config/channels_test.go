package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// channelCfg is a minimal valid config with the given channels block appended.
func channelCfg(channels string) []byte {
	return []byte("server:\n  data_dir: /data/onett\n" + channels)
}

func TestChannelTelegramValid(t *testing.T) {
	cfg, err := Parse(channelCfg(`
channels:
  - type: telegram
    options:
      bot_token_env: TELEGRAM_BOT_TOKEN
      allowed_user_ids: [42, 7]
      state_file: /tmp/tg.json
`), "")
	require.NoError(t, err)
	require.Len(t, cfg.Channels, 1)
	tc, err := ParseTelegramChannel(cfg.Channels[0], cfg.Server.DataDir)
	require.NoError(t, err)
	assert.Equal(t, TelegramChannel{BotTokenEnv: "TELEGRAM_BOT_TOKEN", AllowedUserIDs: []int64{42, 7}, StateFile: "/tmp/tg.json"}, tc)
}

func TestChannelTelegramDefaultStateFile(t *testing.T) {
	cfg, err := Parse(channelCfg(`
channels:
  - type: telegram
    options: {bot_token_env: T, allowed_user_ids: [1]}
`), "")
	require.NoError(t, err)
	tc, err := ParseTelegramChannel(cfg.Channels[0], cfg.Server.DataDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/data/onett", "channels", "telegram.json"), tc.StateFile)
}

func TestChannelValidationErrors(t *testing.T) {
	tests := map[string]struct{ yaml, want string }{
		"unknown type":    {"channels:\n  - type: carrier-pigeon\n", `channels[0].type: unknown channel type "carrier-pigeon"`},
		"empty type":      {"channels:\n  - options: {}\n", "channels[0].type: must not be empty"},
		"missing token":   {"channels:\n  - type: telegram\n    options: {allowed_user_ids: [1]}\n", "channels[0]: options.bot_token_env"},
		"empty allowlist": {"channels:\n  - type: telegram\n    options: {bot_token_env: T, allowed_user_ids: []}\n", "channels[0]: options.allowed_user_ids: must list at least one"},
		"no allowlist":    {"channels:\n  - type: telegram\n    options: {bot_token_env: T}\n", "options.allowed_user_ids"},
		"bad id":          {"channels:\n  - type: telegram\n    options: {bot_token_env: T, allowed_user_ids: [abc]}\n", "not a Telegram user ID"},
		"unknown option":  {"channels:\n  - type: telegram\n    options: {bot_token_env: T, allowed_user_ids: [1], token: x}\n", `unknown option "token"`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(channelCfg(tt.yaml), "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestChannelDiscordValid(t *testing.T) {
	cfg, err := Parse(channelCfg(`
channels:
  - type: discord
    options:
      bot_token_env: DISCORD_BOT_TOKEN
      allowed_user_ids: ["123456789012345678"]
`), "")
	require.NoError(t, err)
	dc, err := ParseDiscordChannel(cfg.Channels[0], cfg.Server.DataDir)
	require.NoError(t, err)
	assert.Equal(t, DiscordChannel{
		BotTokenEnv: "DISCORD_BOT_TOKEN", AllowedUserIDs: []string{"123456789012345678"},
		StateFile: filepath.Join("/data/onett", "channels", "discord.json"),
	}, dc)
}

func TestChannelDiscordValidationErrors(t *testing.T) {
	tests := map[string]struct{ yaml, want string }{
		"missing token":  {"channels:\n  - type: discord\n    options: {allowed_user_ids: ['1']}\n", "options.bot_token_env"},
		"no allowlist":   {"channels:\n  - type: discord\n    options: {bot_token_env: T}\n", "options.allowed_user_ids"},
		"number id":      {"channels:\n  - type: discord\n    options: {bot_token_env: T, allowed_user_ids: [12345]}\n", "quoted string"},
		"non-digit id":   {"channels:\n  - type: discord\n    options: {bot_token_env: T, allowed_user_ids: ['abc']}\n", "not a Discord user ID"},
		"unknown option": {"channels:\n  - type: discord\n    options: {bot_token_env: T, allowed_user_ids: ['1'], token: x}\n", `unknown option "token"`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(channelCfg(tt.yaml), "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}
