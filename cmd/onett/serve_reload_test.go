package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/channel"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/notify"
	"github.com/bobbyrc/onett/internal/secrets"
)

type reloadSink struct {
	mu   sync.Mutex
	seen []string
}

func (s *reloadSink) Notify(_ context.Context, n notify.Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, n.Title)
	return nil
}

func (s *reloadSink) titles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.seen...)
}

type reloadBot struct {
	id  string
	log *reloadLog
}

func (b reloadBot) Run(ctx context.Context) error {
	b.log.add("start " + b.id)
	<-ctx.Done()
	b.log.add("stop " + b.id)
	return ctx.Err()
}

type reloadLog struct {
	mu sync.Mutex
	ev []string
}

func (l *reloadLog) add(s string) { l.mu.Lock(); l.ev = append(l.ev, s); l.mu.Unlock() }
func (l *reloadLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.ev...)
}

// reloadTypeSeq makes the fake channel type's name unique for each run of the test below.
// channel.Register panics on a type registered twice, and the registry lives as long as the
// process, so a fixed name let the test run only once in it (go test -count=2 panicked).
var reloadTypeSeq atomic.Int64

// A reload with a new notifier, a changed channel option and a changed secret takes effect with
// no restart, and a notifier set that cannot be built leaves the old one running.
func TestApplyReloadSwapsNotifiersAndRestartsChannels(t *testing.T) {
	sink := &reloadSink{}
	notify.Register("zz-reload-sink", func(cfg notify.Config) (notify.Notifier, error) {
		if cfg.Options["fail"] == true {
			return nil, assert.AnError
		}
		return sink, nil
	})
	log := &reloadLog{}
	chanType := fmt.Sprintf("zz-reload-chan-%d", reloadTypeSeq.Add(1))
	channel.Register(chanType, channel.Registration{
		SkipApprovalsForNotifier: "zz-reload-sink",
		Factory: func(c config.Channel, env channel.Env) (channel.Starter, error) {
			id, _ := c.Options["id"].(string)
			tok := env.Getenv("ZZ_RELOAD_TOKEN")
			if tok == "" {
				return nil, assert.AnError
			}
			return func(channel.Host) (channel.Bot, error) { return reloadBot{id, log}, nil }, nil
		},
	})

	secretStore := secrets.New(filepath.Join(t.TempDir(), "secrets.env"))
	_, err := secretStore.Set("ZZ_RELOAD_TOKEN", "first")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Unsetenv("ZZ_RELOAD_TOKEN") })

	ctx, cancel := context.WithCancel(context.Background())
	sup := channel.NewSupervisor(ctx, nil, slog.Default())
	t.Cleanup(func() { cancel(); sup.Wait() })
	env := channel.Env{Getenv: os.Getenv}
	notifier := notify.NewSwappable(nil)

	chans := func(id string) []config.Channel {
		return []config.Channel{{Type: chanType, Options: map[string]any{"id": id}}}
	}
	sinks := func(fail bool) []config.Notifier {
		return []config.Notifier{{Type: "zz-reload-sink", Options: map[string]any{"fail": fail}}}
	}
	waitEvents := func(n int) {
		t.Helper()
		require.Eventually(t, func() bool { return len(log.get()) >= n }, time.Second, time.Millisecond)
	}

	// Start with no notifiers: an event goes nowhere.
	require.NoError(t, notifier.Notify(ctx, notify.Notification{Title: "before"}))
	assert.Empty(t, sink.titles())

	// Reload adds a notifier and a channel; the notifier now receives events, with approvals
	// skipped because the channel runs.
	require.NoError(t, applyReload(&config.Config{Channels: chans("c1"), Notify: sinks(false)}, sup, env, notifier, slog.Default()))
	require.NoError(t, notifier.Notify(ctx, notify.Notification{Title: "after"}))
	assert.Equal(t, []string{"after"}, sink.titles())
	waitEvents(1)

	// A changed channel option replaces the bot.
	require.NoError(t, applyReload(&config.Config{Channels: chans("c2"), Notify: sinks(false)}, sup, env, notifier, slog.Default()))
	waitEvents(3)
	assert.Equal(t, []string{"start c1", "stop c1", "start c2"}, log.get())

	// Setting a secret (as the secrets API does before it reloads) restarts the channel even
	// though the config text is identical.
	_, err = secretStore.Set("ZZ_RELOAD_TOKEN", "second")
	require.NoError(t, err)
	require.NoError(t, secretStore.Load())
	require.NoError(t, applyReload(&config.Config{Channels: chans("c2"), Notify: sinks(false)}, sup, env, notifier, slog.Default()))
	waitEvents(5)
	assert.Equal(t, []string{"start c1", "stop c1", "start c2", "stop c2", "start c2"}, log.get())

	// A notifier set that cannot be built is reported, and the old one keeps working.
	err = applyReload(&config.Config{Channels: chans("c2"), Notify: sinks(true)}, sup, env, notifier, slog.Default())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notifiers not reloaded")
	require.NoError(t, notifier.Notify(ctx, notify.Notification{Title: "still"}))
	assert.Equal(t, []string{"after", "still"}, sink.titles())
	assert.Len(t, log.get(), 5, "channels were not touched")
}
