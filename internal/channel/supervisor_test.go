package channel_test

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

// supBot records its lifecycle on a shared event log.
type supBot struct {
	id  string
	log *eventLog
}

func (b supBot) Run(ctx context.Context) error {
	b.log.add("start " + b.id)
	<-ctx.Done()
	b.log.add("stop " + b.id)
	return ctx.Err()
}

type eventLog struct {
	mu sync.Mutex
	ev []string
}

func (l *eventLog) add(s string) { l.mu.Lock(); l.ev = append(l.ev, s); l.mu.Unlock() }
func (l *eventLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.ev...)
}

// registerSupType registers a fake channel type whose bot id is its "id" option plus the value of
// the environment variable named by "token_env".
func registerSupType(t *testing.T, typ string, log *eventLog, failStart *atomic.Bool) {
	t.Helper()
	channel.RegisterForTest(t, typ, channel.Registration{
		SkipApprovalsForNotifier: "log",
		Factory: func(c config.Channel, env channel.Env) (channel.Starter, error) {
			id, _ := c.Options["id"].(string)
			if name, _ := c.Options["token_env"].(string); name != "" {
				if env.Getenv(name) == "" {
					return nil, assert.AnError
				}
			}
			return func(channel.Host) (channel.Bot, error) {
				if failStart != nil && failStart.Load() {
					return nil, assert.AnError
				}
				return supBot{id, log}, nil
			}, nil
		},
	})
}

func newSup(t *testing.T) (*channel.Supervisor, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	sup := channel.NewSupervisor(ctx, nil, slog.Default())
	stop := func() { cancel(); sup.Wait() }
	t.Cleanup(stop)
	return sup, stop
}

func chanCfg(typ, id string) config.Channel {
	return config.Channel{Type: typ, Options: map[string]any{"id": id, "token_env": "SUP_TOKEN"}}
}

func TestSupervisorRestartsOnlyChangedChannels(t *testing.T) {
	log := &eventLog{}
	registerSupType(t, "zz-sup-a", log, nil)
	registerSupType(t, "zz-sup-b", log, nil)
	env := channel.Env{Getenv: func(string) string { return "tok" }}
	sup, stop := newSup(t)

	require.NoError(t, sup.Reconcile([]config.Channel{chanCfg("zz-sup-a", "a1"), chanCfg("zz-sup-b", "b1"), chanCfg("zz-sup-a", "ignored")}, env))
	require.Eventually(t, func() bool { return len(log.get()) == 2 }, time.Second, time.Millisecond)
	assert.Equal(t, map[string]bool{"log": true}, sup.SkipApprovals())

	// Change a's options: the old a stops, the new a starts, b is untouched.
	require.NoError(t, sup.Reconcile([]config.Channel{chanCfg("zz-sup-a", "a2"), chanCfg("zz-sup-b", "b1")}, env))
	require.Eventually(t, func() bool { return len(log.get()) == 4 }, time.Second, time.Millisecond)
	ev := log.get()
	assert.Contains(t, ev[2:], "stop a1")
	assert.Contains(t, ev[2:], "start a2")
	assert.NotContains(t, ev, "stop b1")

	// Removing a channel stops it.
	require.NoError(t, sup.Reconcile([]config.Channel{chanCfg("zz-sup-b", "b1")}, env))
	assert.Contains(t, log.get(), "stop a2", "Reconcile waits for the bot to exit")
	assert.Equal(t, map[string]bool{"log": true}, sup.SkipApprovals())

	stop()
	assert.Contains(t, log.get(), "stop b1")
}

func TestSupervisorRestartsWhenSecretChanges(t *testing.T) {
	log := &eventLog{}
	registerSupType(t, "zz-sup-secret", log, nil)
	var token atomic.Value
	token.Store("one")
	env := channel.Env{Getenv: func(string) string { return token.Load().(string) }}
	sup, _ := newSup(t)
	cfg := []config.Channel{chanCfg("zz-sup-secret", "s")}

	require.NoError(t, sup.Reconcile(cfg, env))
	require.NoError(t, sup.Reconcile(cfg, env))
	require.Eventually(t, func() bool { return len(log.get()) == 1 }, time.Second, time.Millisecond)
	assert.Equal(t, []string{"start s"}, log.get(), "same config and secret: no restart")

	token.Store("two")
	require.NoError(t, sup.Reconcile(cfg, env))
	require.Eventually(t, func() bool { return len(log.get()) == 3 }, time.Second, time.Millisecond)
	assert.Equal(t, []string{"start s", "stop s", "start s"}, log.get())
}

func TestSupervisorBadConfigKeepsOldBots(t *testing.T) {
	log := &eventLog{}
	registerSupType(t, "zz-sup-bad", log, nil)
	var token atomic.Value
	token.Store("one")
	env := channel.Env{Getenv: func(string) string { return token.Load().(string) }}
	sup, _ := newSup(t)
	cfg := []config.Channel{chanCfg("zz-sup-bad", "x")}
	require.NoError(t, sup.Reconcile(cfg, env))

	token.Store("") // the factory now refuses: the token is unset
	require.Error(t, sup.Reconcile(cfg, env))
	require.Error(t, sup.Reconcile([]config.Channel{{Type: "zz-sup-nonexistent"}}, env))
	require.Eventually(t, func() bool { return len(log.get()) == 1 }, time.Second, time.Millisecond)
	assert.Equal(t, []string{"start x"}, log.get(), "the old bot keeps running")
}

func TestSupervisorStartFailureIsNotFatal(t *testing.T) {
	log := &eventLog{}
	var fail atomic.Bool
	fail.Store(true)
	registerSupType(t, "zz-sup-fail", log, &fail)
	registerSupType(t, "zz-sup-ok", log, nil)
	env := channel.Env{Getenv: func(string) string { return "tok" }}
	sup, _ := newSup(t)
	require.NoError(t, sup.Reconcile([]config.Channel{chanCfg("zz-sup-fail", "f"), chanCfg("zz-sup-ok", "o")}, env))
	require.Eventually(t, func() bool { return len(log.get()) == 1 }, time.Second, time.Millisecond)
	assert.Equal(t, []string{"start o"}, log.get())
}
