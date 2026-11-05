package main

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/channel"
	"github.com/bobbyrc/onett/internal/notify"
)

type countBot struct {
	running *atomic.Int32
	started chan struct{}
}

func (b countBot) Run(ctx context.Context) error {
	b.running.Add(1)
	b.started <- struct{}{}
	<-ctx.Done()
	b.running.Add(-1)
	return ctx.Err()
}

type countNotifier struct{ n *atomic.Int32 }

func (c countNotifier) Notify(context.Context, notify.Notification) error {
	c.n.Add(1)
	return nil
}

func TestCommsReloadRestartsBotsAndSwapsNotifier(t *testing.T) {
	var running atomic.Int32
	started := make(chan struct{}, 4)
	cm := &comms{
		notifier: &swapNotifier{cur: countNotifier{new(atomic.Int32)}},
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	cm.starters = []namedStarter{{name: "t", start: func(channel.Host) (channel.Bot, error) {
		return countBot{&running, started}, nil
	}}}

	ctx, cancel := context.WithCancel(context.Background())
	cm.run(ctx, nil)
	<-started

	// A reload with no channels stops the old bot before returning.
	oldNotes := new(atomic.Int32)
	cm.notifier.set(countNotifier{oldNotes})
	cm.mu.Lock()
	cm.restartLocked()
	cm.mu.Unlock()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("bot was not restarted")
	}
	if got := running.Load(); got != 1 {
		t.Fatalf("running bots = %d, want 1 (old one must have stopped)", got)
	}

	newNotes := new(atomic.Int32)
	cm.notifier.set(countNotifier{newNotes})
	_ = cm.notifier.Notify(context.Background(), notify.Notification{})
	if oldNotes.Load() != 0 || newNotes.Load() != 1 {
		t.Fatalf("notify went old=%d new=%d, want 0/1", oldNotes.Load(), newNotes.Load())
	}

	cancel()
	cm.wait()
	if got := running.Load(); got != 0 {
		t.Fatalf("running bots after shutdown = %d", got)
	}
}
