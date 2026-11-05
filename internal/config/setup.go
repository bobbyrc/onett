package config

import (
	"fmt"
	"strconv"
	"strings"
)

// The functions here build the config entries that `onett init` and the web UI's guided setup
// both write, so the two cannot drift apart. They hold no secrets: a token is named by the
// environment variable it is read from.

// NtfyTokenEnv is the variable an ntfy notifier's access token is read from.
const NtfyTokenEnv = "NTFY_TOKEN"

// ChannelTokenEnv is the environment variable the bot token of a channel type is read from
// (TELEGRAM_BOT_TOKEN, DISCORD_BOT_TOKEN).
func ChannelTokenEnv(channel string) string { return strings.ToUpper(channel) + "_BOT_TOKEN" }

// ChannelEntry is the channels entry for a Telegram or Discord bot that obeys userIDs. Telegram
// IDs are numbers; Discord IDs are 64-bit snowflakes and must stay strings.
func ChannelEntry(channel string, userIDs []string) (Channel, error) {
	var ids []any
	for _, id := range userIDs {
		switch channel {
		case "telegram":
			n, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return Channel{}, fmt.Errorf("telegram user ID %q must be a whole number", id)
			}
			ids = append(ids, int(n))
		case "discord":
			ids = append(ids, id)
		default:
			return Channel{}, fmt.Errorf("channel %q: use telegram or discord", channel)
		}
	}
	return Channel{Type: channel, Options: map[string]any{
		"bot_token_env":    ChannelTokenEnv(channel),
		"allowed_user_ids": ids,
	}}, nil
}

// NtfyEntry is the notify entry for an ntfy topic. An empty server means https://ntfy.sh.
func NtfyEntry(topic, server string) Notifier {
	opts := map[string]any{"topic": topic}
	if server != "" {
		opts["server"] = server
	}
	return Notifier{Type: "ntfy", Options: opts}
}
