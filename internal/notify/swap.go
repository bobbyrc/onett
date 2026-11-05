package notify

import (
	"context"
	"sync/atomic"
)

// Swappable is a Notifier whose target can be replaced while the daemon runs. A Notify call
// takes the current target once, so a send already in flight finishes on the old one, and the
// next call goes to the new one: nothing is lost and nothing is sent twice.
type Swappable struct {
	cur atomic.Pointer[Notifier]
}

var _ Notifier = (*Swappable)(nil)

// NewSwappable returns a Swappable that starts with n (nil means a no-op).
func NewSwappable(n Notifier) *Swappable {
	s := &Swappable{}
	s.Swap(n)
	return s
}

// Swap makes n the target for later calls. A nil n turns notifications off.
func (s *Swappable) Swap(n Notifier) {
	if n == nil {
		n = noop{}
	}
	s.cur.Store(&n)
}

// Notify sends n through the current target.
func (s *Swappable) Notify(ctx context.Context, n Notification) error {
	return (*s.cur.Load()).Notify(ctx, n)
}

type noop struct{}

func (noop) Notify(context.Context, Notification) error { return nil }
