package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	"github.com/bobbyrc/onett/internal/notify"
	"github.com/bobbyrc/onett/internal/store"
)

// A harness that hits its subscription or provider limit is held: no new run starts on it until
// the reset time, so the factory waits the limit out instead of failing tasks against it.
// Holds live in memory. After a restart the next run on the harness finds the limit again.
const (
	holdBackoffStart = 5 * time.Minute // hold when the harness gave no reset time, first hit
	holdBackoffMax   = time.Hour
)

// holdMargin is added to a reported reset time, so the first run after the hold does not race
// the harness's own clock. A variable so tests can shrink it.
var holdMargin = time.Minute

// holdTable is the set of held harnesses. The zero value is ready.
type holdTable struct {
	mu sync.Mutex
	m  map[string]domain.HarnessHold
}

// hit records a limit hit on name and returns the hold. The hold lasts until the reset time plus
// holdMargin; with no usable reset time it backs off from holdBackoffStart, doubling per
// consecutive hit up to holdBackoffMax. A longer hold already in place is kept.
func (t *holdTable) hit(name string, rl harness.RateLimit, now time.Time) domain.HarnessHold {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.m == nil {
		t.m = map[string]domain.HarnessHold{}
	}
	prev := t.m[name]
	h := domain.HarnessHold{Harness: name, Message: rl.Message, Hits: prev.Hits + 1}
	if rl.ResetAt.After(now) {
		h.Until = rl.ResetAt.Add(holdMargin)
	} else {
		wait := holdBackoffStart
		for i := 1; i < h.Hits && wait < holdBackoffMax; i++ {
			wait *= 2
		}
		h.Until = now.Add(min(wait, holdBackoffMax))
	}
	if prev.Until.After(h.Until) {
		h.Until = prev.Until
	}
	t.m[name] = h
	return h
}

// clear forgets name's hits after a run on it ended without a limit, so the next limit backs off
// from the start again.
func (t *holdTable) clear(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, name)
}

// active lists the holds still in force at now, by harness name.
func (t *holdTable) active(now time.Time) []domain.HarnessHold {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []domain.HarnessHold
	for _, h := range t.m {
		if h.Until.After(now) {
			out = append(out, h)
		}
	}
	slices.SortFunc(out, func(a, b domain.HarnessHold) int { return strings.Compare(a.Harness, b.Harness) })
	return out
}

// held returns the hold on name if one is in force at now.
func (t *holdTable) held(name string, now time.Time) (domain.HarnessHold, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	h, ok := t.m[name]
	return h, ok && h.Until.After(now)
}

// Holds implements api.Controller: the harnesses currently held for a rate limit.
func (e *Engine) Holds() []domain.HarnessHold { return e.holds.active(e.d.Now()) }

// noteRateLimit holds a harness after a run stopped on its limit, and says so once per hold.
func (e *Engine) noteRateLimit(ctx context.Context, name string, rl harness.RateLimit) domain.HarnessHold {
	now := e.d.Now()
	_, already := e.holds.held(name, now)
	h := e.holds.hit(name, rl, now)
	if !already {
		msg := holdSummary(h, now)
		e.d.Bus.Emit(ctx, domain.EvSystem, "", "", "", msg, nil)
		e.notify(ctx, notify.PriorityLow, "Harness held: "+name, msg+"\n\n"+rl.Message, "/")
	}
	return h
}

// holdSummary is "claude held: rate limit, resets 15:04 (37m left)".
func holdSummary(h domain.HarnessHold, now time.Time) string {
	return fmt.Sprintf("%s held: rate limit, resets %s (%s left)", h.Harness,
		h.Until.Local().Format("15:04"), h.Until.Sub(now).Round(time.Minute))
}

// requeueHeld puts a task whose run was stopped by a rate limit back in the queue. The attempt
// does not count: ClaimNextTask had incremented Attempts, so it is given back, and the task is
// never blocked for it.
func (e *Engine) requeueHeld(ctx context.Context, id string, h domain.HarnessHold) {
	msg := fmt.Sprintf("held: %s rate limit, resets %s", h.Harness, h.Until.Local().Format("15:04"))
	if _, _, err := e.setTaskFrom(ctx, id, fromRunning, msg, func(t *domain.Task) error {
		t.Status = domain.TaskQueued
		if t.Attempts > 0 {
			t.Attempts--
		}
		return nil
	}); err != nil {
		e.d.Logger.Error("hold task", "task", id, "err", err)
	}
}

// heldRoles lists the (project, role) pairs whose agent runs on a held harness. The scheduler
// passes them to ClaimNextTask so those tasks wait while tasks on other harnesses go on.
func (e *Engine) heldRoles() []store.ProjectRole {
	holds := e.holds.active(e.d.Now())
	if len(holds) == 0 {
		return nil
	}
	cfg := e.Config()
	var out []store.ProjectRole
	for name := range cfg.Projects {
		for role, a := range cfg.AgentsFor(name) {
			if _, ok := e.holds.held(a.Harness, e.d.Now()); ok && !a.Disabled {
				out = append(out, store.ProjectRole{Project: name, Role: role})
			}
		}
	}
	return out
}
