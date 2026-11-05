package telegram

import (
	"fmt"
	"strings"

	"github.com/bobbyrc/onett/internal/channel"
	"github.com/bobbyrc/onett/internal/config"
)

var _ Host = channel.Host(nil)

func init() {
	channel.Register("telegram", channel.Registration{
		Factory:                  factory,
		SkipApprovalsForNotifier: "telegram",
	})
}

// factory validates the telegram channel options and reads the token from the environment variable
// the config names; an empty variable is an error. Only the variable's name is ever put in a
// message.
func factory(c config.Channel, env channel.Env) (channel.Starter, error) {
	cc, err := config.ParseTelegramChannel(c, env.DataDir)
	if err != nil {
		return nil, fmt.Errorf("channel telegram: %w", err)
	}
	token := strings.TrimSpace(env.Getenv(cc.BotTokenEnv))
	if token == "" {
		return nil, fmt.Errorf("channel telegram: environment variable %q (bot_token_env) is empty", cc.BotTokenEnv)
	}
	opts := Options{Token: token, AllowedUserIDs: cc.AllowedUserIDs, StateFile: cc.StateFile}
	return func(host channel.Host) (channel.Bot, error) {
		bot, err := New(opts, host, env.Logger)
		if err != nil {
			return nil, err
		}
		return bot, nil
	}, nil
}
