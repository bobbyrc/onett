package config

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
)

// Channel is one entry of the channels list: a two-way link between the owner and onett, such as
// a Telegram bot. Unlike a notifier it also reads what the owner sends.
type Channel struct {
	Type    string         `yaml:"type" json:"type"`
	Options map[string]any `yaml:"options" json:"options,omitempty"`
}

// TelegramChannel is the parsed options of a `type: telegram` channel.
type TelegramChannel struct {
	BotTokenEnv    string  // name of the environment variable that holds the bot token
	AllowedUserIDs []int64 // Telegram user IDs that onett obeys
	StateFile      string  // JSON file the bot owns
}

// telegramChannelOptions are the keys a telegram channel accepts.
var telegramChannelOptions = map[string]bool{"bot_token_env": true, "allowed_user_ids": true, "state_file": true}

// ParseTelegramChannel reads and validates the options of a telegram channel. dataDir is used for
// the default state file, <dataDir>/channels/telegram.json. The token itself is never read here:
// BotTokenEnv only names the variable.
func ParseTelegramChannel(c Channel, dataDir string) (TelegramChannel, error) {
	var out TelegramChannel
	var unknown []string
	for k := range c.Options {
		if !telegramChannelOptions[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return out, fmt.Errorf("unknown option %q", unknown[0])
	}
	if v, ok := c.Options["bot_token_env"]; ok && v != nil {
		out.BotTokenEnv = strings.TrimSpace(fmt.Sprint(v))
	}
	if out.BotTokenEnv == "" {
		return out, fmt.Errorf("options.bot_token_env: must name the environment variable that holds the bot token")
	}
	raw, _ := c.Options["allowed_user_ids"].([]any)
	if len(raw) == 0 {
		return out, fmt.Errorf("options.allowed_user_ids: must list at least one Telegram user ID")
	}
	for _, v := range raw {
		id, ok := toInt64(v)
		if !ok || id == 0 {
			return out, fmt.Errorf("options.allowed_user_ids: %v is not a Telegram user ID (a whole number)", v)
		}
		out.AllowedUserIDs = append(out.AllowedUserIDs, id)
	}
	if v, ok := c.Options["state_file"]; ok && v != nil {
		out.StateFile = strings.TrimSpace(fmt.Sprint(v))
	}
	if out.StateFile == "" {
		out.StateFile = filepath.Join(dataDir, "channels", "telegram.json")
	}
	return out, nil
}

// toInt64 accepts the number types YAML and JSON decoding produce.
func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case uint64:
		return int64(n), n <= math.MaxInt64
	case float64:
		return int64(n), n == math.Trunc(n) && math.Abs(n) < 1<<53
	}
	return 0, false
}

// validateChannels reports problems with the channels list through add.
func validateChannels(c *Config, add func(string, ...any)) {
	for i, ch := range c.Channels {
		prefix := fmt.Sprintf("channels[%d]", i)
		switch ch.Type {
		case "":
			add("%s.type: must not be empty", prefix)
		case "telegram":
			if _, err := ParseTelegramChannel(ch, c.Server.DataDir); err != nil {
				add("%s: %v", prefix, err)
			}
		default:
			add("%s.type: unknown channel type %q (known: telegram)", prefix, ch.Type)
		}
	}
}
