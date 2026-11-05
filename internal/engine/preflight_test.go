package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/notify"
	"github.com/bobbyrc/onett/internal/store/sqlite"
)

type countNotifier struct {
	mu sync.Mutex
	n  []notify.Notification
}

func (c *countNotifier) Notify(_ context.Context, n notify.Notification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n = append(c.n, n)
	return nil
}

func (c *countNotifier) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.n)
}

func newPreflightEngine(t *testing.T, read func(string) error) (*Engine, *config.Project, *countNotifier, *time.Time) {
	t.Helper()
	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	n := &countNotifier{}
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := &Engine{
		d: Deps{
			Bus:      events.NewBus(st, slog.New(slog.NewTextHandler(io.Discard, nil))),
			Notifier: n,
			Now:      func() time.Time { return clock },
		},
		cfg: &config.Config{},
		pre: newPreflight(),
	}
	e.pre.readDir = read
	e.pre.timeout = 20 * time.Millisecond
	return e, &config.Project{Name: "demo", Path: t.TempDir()}, n, &clock
}

func TestPreflightTimeoutBlocksOnceAndRecovers(t *testing.T) {
	block := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(block) }) }
	defer release()
	e, p, n, clock := newPreflightEngine(t, func(string) error {
		<-block // simulates a read stuck in the kernel
		return nil
	})

	res := e.checkPreflight(context.Background(), p)
	require.False(t, res.OK)
	assert.NotEmpty(t, res.Path)
	assert.NotEmpty(t, res.Fix)
	assert.Equal(t, 1, n.count())

	// Later failures in the same episode do not notify again.
	*clock = clock.Add(time.Minute)
	assert.False(t, e.checkPreflight(context.Background(), p).OK)
	assert.Equal(t, 1, n.count())

	// Once the blocked reads return, the next check passes.
	release()
	assert.Eventually(t, func() bool {
		*clock = clock.Add(time.Minute)
		return e.checkPreflight(context.Background(), p).OK
	}, time.Second, time.Millisecond)
}

func TestPreflightPassIsCached(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	e, p, _, clock := newPreflightEngine(t, func(string) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	})
	require.True(t, e.checkPreflight(context.Background(), p).OK)
	mu.Lock()
	first := calls
	mu.Unlock()
	require.Positive(t, first)

	*clock = clock.Add(30 * time.Second)
	require.True(t, e.checkPreflight(context.Background(), p).OK)
	mu.Lock()
	assert.Equal(t, first, calls, "a pass is cached for a minute")
	mu.Unlock()

	*clock = clock.Add(time.Minute)
	require.True(t, e.checkPreflight(context.Background(), p).OK)
	mu.Lock()
	assert.Greater(t, calls, first)
	mu.Unlock()
}

func TestPreflightErrorFails(t *testing.T) {
	e, p, _, _ := newPreflightEngine(t, func(string) error { return errors.New("operation not permitted") })
	res := e.checkPreflight(context.Background(), p)
	assert.False(t, res.OK)
	assert.Contains(t, res.Error, "operation not permitted")
	assert.Len(t, e.Preflight(), 1)
}

// A task for a project whose tool directories hang stays queued, with no run and no attempt, and
// starts by itself once the read works again.
func TestSchedulerHoldsTasksWhilePreflightFails(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	healthy := false
	env := rtNewEngine(t, rtOpts{preflightRead: func(string) error {
		mu.Lock()
		ok := healthy
		mu.Unlock()
		if !ok {
			<-release
		}
		return nil
	}})
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	task := env.create(domain.Task{})

	time.Sleep(300 * time.Millisecond)
	got := env.task(task.ID)
	assert.Equal(t, domain.TaskQueued, got.Status)
	assert.Zero(t, got.Attempts)
	assert.Empty(t, env.runs(task.ID))
	require.Len(t, env.e.Preflight(), 1)
	assert.False(t, env.e.Preflight()[0].OK)

	mu.Lock()
	healthy = true
	mu.Unlock()
	once.Do(func() { close(release) })
	env.e.resetPreflight()
	env.e.wakeScheduler()
	rtWait(t, 20*time.Second, "task to start once the probe passes", func() bool {
		return len(env.runs(task.ID)) > 0
	})
}
