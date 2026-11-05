package engine

import (
	"context"
	"time"
)

// housekeepEvery is how often housekeep runs while the engine is up.
const housekeepEvery = 24 * time.Hour

// housekeepLoop runs housekeep at start and then once a day until ctx is done.
func (e *Engine) housekeepLoop(ctx context.Context) {
	ticker := time.NewTicker(housekeepEvery)
	defer ticker.Stop()
	for {
		e.housekeep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// housekeep deletes event and run log rows older than the server's retention windows. A window
// of zero is never set, because ApplyDefaults fills it, so every window is applied. Problems are
// logged and never returned: a failed prune must not stop the engine, and the next run retries.
func (e *Engine) housekeep(ctx context.Context) {
	srv := e.Config().Server
	now := e.d.Now()
	if srv.RetainEvents.Duration > 0 {
		before := now.Add(-srv.RetainEvents.Duration)
		n, err := e.d.Store.PruneEvents(ctx, before)
		switch {
		case err != nil:
			e.d.Logger.Warn("prune events", "before", before, "err", err)
		case n > 0:
			e.d.Logger.Info("pruned old events", "deleted", n, "before", before)
		}
	}
	if srv.RetainRunLogs.Duration > 0 {
		before := now.Add(-srv.RetainRunLogs.Duration)
		n, err := e.d.Store.PruneRunLogs(ctx, before)
		switch {
		case err != nil:
			e.d.Logger.Warn("prune run logs", "before", before, "err", err)
		case n > 0:
			e.d.Logger.Info("pruned old run logs", "deleted", n, "before", before)
		}
	}
	e.pruneAttachments(ctx, now)
}

// pruneAttachments deletes chat attachments older than chat.attachments.retention: the files, the
// records, and the conversation directories left empty. A message that referenced one keeps
// rendering without it.
func (e *Engine) pruneAttachments(ctx context.Context, now time.Time) {
	keep := e.Config().Chat.Attachments.Retention.Duration
	if e.d.Attachments == nil || keep <= 0 {
		return
	}
	before := now.Add(-keep)
	n, err := e.d.Attachments.Prune(ctx, before)
	if err != nil {
		e.d.Logger.Warn("prune attachments", "before", before, "err", err)
	}
	if n > 0 {
		e.d.Logger.Info("pruned old attachments", "deleted", n, "before", before)
	}
}
