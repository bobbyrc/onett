package channel_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/channel"
	"github.com/bobbyrc/onett/internal/config"
)

// testerBot is a bot that can send a test message.
type testerBot struct {
	supBot
	sent *eventLog
	err  error
}

func (b testerBot) SendTest(context.Context) error { b.sent.add("test " + b.id); return b.err }

func registerTesterType(t *testing.T, typ string, sent *eventLog, err error) {
	t.Helper()
	channel.RegisterForTest(t, typ, channel.Registration{
		Factory: func(c config.Channel, env channel.Env) (channel.Starter, error) {
			if env.Getenv("SUP_TOKEN") == "" {
				return nil, assert.AnError
			}
			id, _ := c.Options["id"].(string)
			return func(channel.Host) (channel.Bot, error) {
				return testerBot{supBot{id, &eventLog{}}, sent, err}, nil
			}, nil
		},
	})
}

func TestSupervisorTestSendsThroughRunningBot(t *testing.T) {
	sent := &eventLog{}
	registerTesterType(t, "zz-test-ok", sent, nil)
	registerSupType(t, "zz-test-plain", &eventLog{}, nil)
	env := channel.Env{Getenv: func(string) string { return "tok" }}
	sup, _ := newSup(t)
	require.NoError(t, sup.Reconcile([]config.Channel{chanCfg("zz-test-ok", "a"), chanCfg("zz-test-plain", "b")}, env))

	require.NoError(t, sup.Test(context.Background(), chanCfg("zz-test-ok", "a"), env))
	assert.Equal(t, []string{"test a"}, sent.get())

	// A bot without SendTest: 501 material.
	err := sup.Test(context.Background(), chanCfg("zz-test-plain", "b"), env)
	assert.ErrorIs(t, err, channel.ErrNoTest)
}

func TestSupervisorTestReportsBotError(t *testing.T) {
	registerTesterType(t, "zz-test-fail", &eventLog{}, assert.AnError)
	env := channel.Env{Getenv: func(string) string { return "tok" }}
	sup, _ := newSup(t)
	require.NoError(t, sup.Reconcile([]config.Channel{chanCfg("zz-test-fail", "a")}, env))
	assert.ErrorIs(t, sup.Test(context.Background(), chanCfg("zz-test-fail", "a"), env), assert.AnError)
}

func TestSupervisorTestNotRunningSaysWhy(t *testing.T) {
	registerTesterType(t, "zz-test-down", &eventLog{}, nil)
	env := channel.Env{Getenv: func(string) string { return "" }} // token unset
	sup, _ := newSup(t)
	_ = sup.Reconcile([]config.Channel{chanCfg("zz-test-down", "a")}, env)

	err := sup.Test(context.Background(), chanCfg("zz-test-down", "a"), env)
	require.ErrorIs(t, err, channel.ErrNotRunning)
	assert.Contains(t, err.Error(), assert.AnError.Error(), "the reason from resolving the channel")

	// Token present but the bot never started (not reconciled): still not running.
	env = channel.Env{Getenv: func(string) string { return "tok" }}
	err = sup.Test(context.Background(), chanCfg("zz-test-down", "a"), env)
	assert.ErrorIs(t, err, channel.ErrNotRunning)
}
