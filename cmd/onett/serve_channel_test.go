package main

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/channel"
	"github.com/bobbyrc/onett/internal/config"
)

// resolve runs resolveChannels with a fixed environment.
func resolve(t *testing.T, cfg *config.Config) ([]namedStarter, map[string]bool, error) {
	t.Helper()
	return resolveChannels(cfg, slog.Default())
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
	starters, skip, err := resolve(t, cfg)
	require.NoError(t, err)
	require.Len(t, starters, 1)
	assert.Equal(t, "telegram", starters[0].name)
	assert.Equal(t, map[string]bool{"telegram": true}, skip)

	starters, skip, err = resolve(t, &config.Config{})
	require.NoError(t, err)
	assert.Empty(t, starters)
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
	starters, skip, err := resolve(t, cfg)
	require.NoError(t, err)
	require.Len(t, starters, 1)
	assert.Equal(t, "discord", starters[0].name)
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

type fakeBot struct{ started, stopped *atomic.Bool }

func (b fakeBot) Run(ctx context.Context) error {
	b.started.Store(true)
	<-ctx.Done()
	b.stopped.Store(true)
	return ctx.Err()
}

// A channel type registered by a test plugs into serve's wiring without serve knowing it.
func TestResolveChannelsUsesRegisteredFakeType(t *testing.T) {
	var started, stopped atomic.Bool
	channel.Register("zz-fake", channel.Registration{
		SkipApprovalsForNotifier: "log",
		Factory: func(c config.Channel, env channel.Env) (channel.Starter, error) {
			return func(channel.Host) (channel.Bot, error) { return fakeBot{&started, &stopped}, nil }, nil
		},
	})
	cfg := &config.Config{Channels: []config.Channel{{Type: "zz-fake"}, {Type: "zz-fake"}}}
	starters, skip, err := resolve(t, cfg)
	require.NoError(t, err)
	require.Len(t, starters, 1, "only the first channel of a type is used")
	assert.Equal(t, map[string]bool{"log": true}, skip)

	out := notifyConfigs([]config.Notifier{{Type: "log"}, {Type: "telegram"}}, skip)
	assert.Equal(t, true, out[0].Options["skip_approvals"])
	assert.NotContains(t, out[1].Options, "skip_approvals")

	ctx, cancel := context.WithCancel(context.Background())
	var bots sync.WaitGroup
	startBots(ctx, &bots, nil, starters, slog.Default())
	require.Eventually(t, started.Load, time.Second, time.Millisecond)
	assert.False(t, stopped.Load())
	cancel()
	bots.Wait()
	assert.True(t, stopped.Load())
}
