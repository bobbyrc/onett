package notify

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type swapRecorder struct {
	mu   sync.Mutex
	seen []string
}

func (r *swapRecorder) Notify(_ context.Context, n Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, n.Title)
	return nil
}

func TestSwappableStartsEmptyThenReachesNewNotifier(t *testing.T) {
	s := NewSwappable(nil)
	require.NoError(t, s.Notify(context.Background(), Notification{Title: "dropped"}))

	rec := &swapRecorder{}
	s.Swap(rec)
	require.NoError(t, s.Notify(context.Background(), Notification{Title: "hello"}))
	assert.Equal(t, []string{"hello"}, rec.seen)
}

func TestSwappableInFlightSendFinishesOnOldTarget(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	old := &blocking{started: started, release: release}
	s := NewSwappable(old)

	done := make(chan struct{})
	go func() {
		_ = s.Notify(context.Background(), Notification{Title: "slow"})
		close(done)
	}()
	<-started
	fresh := &swapRecorder{}
	s.Swap(fresh)
	require.NoError(t, s.Notify(context.Background(), Notification{Title: "next"}))
	close(release)
	<-done

	assert.Equal(t, []string{"slow"}, old.rec.seen)
	assert.Equal(t, []string{"next"}, fresh.seen)
}

type blocking struct {
	started, release chan struct{}
	rec              swapRecorder
}

func (b *blocking) Notify(ctx context.Context, n Notification) error {
	close(b.started)
	<-b.release
	return b.rec.Notify(ctx, n)
}
