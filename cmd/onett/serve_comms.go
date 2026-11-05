package main

import (
	"context"
	"log/slog"
	"sync"

	"github.com/bobbyrc/onett/internal/channel"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/notify"
)

// swapNotifier is the notifier the engine holds. Config reload replaces what it forwards to, so
// a changed webhook or token takes effect without a restart.
type swapNotifier struct {
	mu  sync.RWMutex
	cur notify.Notifier
}

var _ notify.Notifier = (*swapNotifier)(nil)

func (s *swapNotifier) Notify(ctx context.Context, n notify.Notification) error {
	s.mu.RLock()
	cur := s.cur
	s.mu.RUnlock()
	return cur.Notify(ctx, n)
}

func (s *swapNotifier) set(n notify.Notifier) {
	s.mu.Lock()
	s.cur = n
	s.mu.Unlock()
}

// comms owns the channel bots and the notifier, so a config reload can rebuild both. A reload
// stops the running bots, waits for them to end (two bots polling one token would fight), and
// starts new ones from the new config.
type comms struct {
	notifier *swapNotifier
	logger   *slog.Logger

	mu       sync.Mutex
	starters []namedStarter
	host     channel.Host
	parent   context.Context // nil until run is called
	cancel   context.CancelFunc
	bots     sync.WaitGroup
}

// rebuild resolves the channels and notifiers of cfg. Nothing changes if either fails to build
// (an empty token variable, say), so a bad reload keeps the working setup.
func (c *comms) rebuild(cfg *config.Config) error {
	starters, skipApprovals, err := resolveChannels(cfg, c.logger)
	if err != nil {
		return err
	}
	fan, err := notify.NewFanout(notifyConfigs(cfg.Notify, skipApprovals), c.logger)
	if err != nil {
		return err
	}
	c.notifier.set(fan)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.starters = starters
	if c.parent != nil {
		c.restartLocked()
	}
	return nil
}

// run starts the bots; they stop when ctx ends. Call wait to join them.
func (c *comms) run(ctx context.Context, host channel.Host) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.parent, c.host = ctx, host
	c.restartLocked()
}

func (c *comms) restartLocked() {
	if c.cancel != nil {
		c.cancel()
		c.bots.Wait()
	}
	ctx, cancel := context.WithCancel(c.parent)
	c.cancel = cancel
	startBots(ctx, &c.bots, c.host, c.starters, c.logger)
}

// wait blocks until the running bots have stopped (their context must be cancelled first).
func (c *comms) wait() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bots.Wait()
}
