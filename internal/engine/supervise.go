package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
)

// logFlushLines and logFlushEvery bound how long a log line waits before it is stored and
// published: whichever comes first.
const (
	logFlushLines = 50
	logFlushEvery = 300 * time.Millisecond
)

// supervise streams a session into the run's log (store + run.log events), enforces the stall
// and denial rules (cancelling the session and recording the reason on ar), waits for the
// session, and stores the run's final state. onEvent (may be nil) sees every event. It returns
// the harness result, the run's final status and the reason ("" for a normal end).
//
// Denials and stalls cancel the run's context (ar.cancel), which kills the session. The reason
// is recorded on ar first, so the status below reflects why the run ended.
//
// A run behind a shim has two more duties. It stores the shim stream offset of the last stored
// line, with last_output_at, after each flush, so a restarted onett can reattach without losing
// or repeating a line. And when onett shuts down (ar.detach) it lets go of the agent instead of
// killing it and returns domain.RunRunning without touching the run: it is still running. A run
// that ar.reattached is seeded with its stored last_output_at, so the stall clock carries over.
func (e *Engine) supervise(ctx context.Context, run domain.Run, sess harness.Session, ar *activeRun, onEvent func(harness.Event)) (harness.Result, domain.RunStatus, string) {
	bg := context.WithoutCancel(ctx)
	limits := e.Config().Limits
	stall := limits.StallTimeout.Duration

	var lastOutput atomic.Int64 // unix nanos of the last event, or the start time
	seed := e.d.Now()
	if ar.reattached && !run.LastOutputAt.IsZero() {
		seed = run.LastOutputAt
	}
	lastOutput.Store(seed.UnixNano())
	done := make(chan struct{})

	// Kill the session when the run's context ends (timeout, cancellation, stall, denial), or
	// let it go on running when onett itself is shutting down.
	go func() {
		select {
		case <-ctx.Done():
			if d, ok := sess.(harness.Detacher); ok && ar.detach.Load() && d.Detach() {
				return
			}
			sess.Cancel()
		case <-done:
		}
	}()

	// Stall watchdog: no output for stall means the run is stuck.
	if stall > 0 {
		interval := min(time.Second, stall/4)
		go func() {
			tick := time.NewTicker(interval)
			defer tick.Stop()
			for {
				select {
				case <-done:
					return
				case <-ctx.Done():
					return
				case <-tick.C:
					idle := e.d.Now().Sub(time.Unix(0, lastOutput.Load()))
					if idle > stall {
						ar.reason.CompareAndSwap(nil, fmt.Sprintf("stalled: no output for %s", stall))
						ar.cancel()
						return
					}
				}
			}
		}()
	}

	// streamed is the shim offset after the last event received; saved is the last one stored.
	var streamed, saved int64
	flush := func(lines []domain.LogLine) {
		e.storeRunLogs(bg, run, lines)
		// The lines are stored now, so the offset after them may be too. A hard stop between the
		// two writes repeats at most this one batch after reattach, never skips one.
		if len(lines) > 0 || streamed > saved {
			saved = streamed
			e.saveProgress(bg, run.ID, saved, time.Unix(0, lastOutput.Load()))
		}
	}

	ticker := time.NewTicker(logFlushEvery)
	defer ticker.Stop()
	events := sess.Events()
	var batch []domain.LogLine
	savedSession := false
	for events != nil {
		select {
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			now := e.d.Now()
			lastOutput.Store(now.UnixNano())
			if ev.Offset > streamed {
				streamed = ev.Offset
			}
			if ev.SessionID != "" && !savedSession {
				savedSession = true
				e.updateRun(bg, run.ID, func(r *domain.Run) error {
					r.SessionID = ev.SessionID
					return nil
				})
			}
			if ev.Kind == domain.LogDenial {
				ar.reason.CompareAndSwap(nil, "denied: "+ev.Text)
				ar.cancel()
			}
			at := ev.Time
			if at.IsZero() {
				at = now
			}
			batch = append(batch, domain.LogLine{RunID: run.ID, Time: at, Kind: ev.Kind, Text: ev.Text, ToolID: ev.ToolID})
			if onEvent != nil {
				onEvent(ev)
			}
			if len(batch) >= logFlushLines {
				flush(batch)
				batch = nil
			}
		case <-ticker.C:
			flush(batch)
			batch = nil
		}
	}
	flush(batch)

	res, err := sess.Wait()
	close(done)
	if errors.Is(err, harness.ErrDetached) {
		return res, domain.RunRunning, ""
	}

	reason := reasonOf(ar)
	if reason == "" && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		reason = fmt.Sprintf("timed out after %s", limits.RunTimeout.Duration)
	}

	status := domain.RunSucceeded
	switch {
	case strings.HasPrefix(reason, "denied"):
		status = domain.RunDenied
	case strings.HasPrefix(reason, "stalled"):
		status = domain.RunStalled
	case strings.HasPrefix(reason, "timed out"):
		status = domain.RunTimedOut
	case reason != "":
		status = domain.RunCancelled
	case len(res.Denials) > 0:
		status = domain.RunDenied
		reason = "denied: " + strings.Join(res.Denials, "; ")
	case res.RateLimit != nil:
		// Not a failure: the harness's limit ended the run. The caller holds the harness and
		// re-queues the task (or tells the chat). Recorded as cancelled, with the harness's words.
		status = domain.RunCancelled
		reason = "rate limited: " + res.RateLimit.Message
	case err != nil || res.IsError:
		status = domain.RunFailed
	}
	if res.RateLimit == nil && status == domain.RunSucceeded {
		e.clearHold(ctx, run.Harness)
	}

	var errMsg string
	switch {
	case status != domain.RunFailed:
		errMsg = reason
	case err != nil:
		errMsg = err.Error()
	case res.FinalText != "":
		errMsg = "agent ended with an error: " + lastChars(res.FinalText, 1000)
	default:
		errMsg = fmt.Sprintf("agent exited with code %d", res.ExitCode)
	}

	endedAt := e.d.Now().UTC()
	lastOut := time.Unix(0, lastOutput.Load()).UTC()
	e.updateRun(bg, run.ID, func(r *domain.Run) error {
		r.Status = status
		r.EndedAt = &endedAt
		r.ExitCode = res.ExitCode
		r.CostUSD = res.CostUSD
		r.CostEstimated = false
		r.Billing = res.Billing
		if b := e.Config().Harness[run.Harness].Billing; b == harness.BillingMetered || b == harness.BillingSubscription {
			r.Billing = b
		}
		if p := e.Config().Harness[run.Harness].Price; res.CostUSD == 0 && p != nil {
			if est := p.Estimate(res.InputTokens, res.CachedInputTokens, res.OutputTokens); est > 0 {
				r.CostUSD, r.CostEstimated = est, true
			}
		}
		r.InputTokens = res.InputTokens
		r.OutputTokens = res.OutputTokens
		r.CachedInputTokens = res.CachedInputTokens
		r.CacheWriteTokens = res.CacheWriteTokens
		if res.SessionID != "" {
			r.SessionID = res.SessionID
		}
		r.Error = errMsg
		r.LastOutputAt = lastOut
		return nil
	})
	return res, status, reason
}

// saveProgress stores how far into the run's shim output the log goes and when it last produced
// any. It adds nothing but those two fields, and publishes nothing.
func (e *Engine) saveProgress(ctx context.Context, runID string, offset int64, lastOutput time.Time) {
	_, err := e.d.Store.UpdateRun(ctx, runID, func(r *domain.Run) error {
		if offset > r.StreamOffset {
			r.StreamOffset = offset
		}
		if lastOutput.After(r.LastOutputAt) || r.LastOutputAt.IsZero() {
			r.LastOutputAt = lastOutput.UTC()
		}
		return nil
	})
	if err != nil {
		e.d.Logger.Error("save run progress", "run", runID, "err", err)
	}
}

// storeRunLogs appends lines to the run's log and tells subscribers.
func (e *Engine) storeRunLogs(ctx context.Context, run domain.Run, lines []domain.LogLine) {
	if len(lines) == 0 {
		return
	}
	stored, err := e.d.Store.AppendRunLogs(ctx, run.ID, lines)
	if err != nil {
		e.d.Logger.Error("store run log", "run", run.ID, "err", err)
		return
	}
	e.d.Bus.Emit(ctx, domain.EvRunLog, run.Project, run.TaskID, run.ID, "", stored)
}

// appendRunLog stores one line of kind on the run's log, as supervise does for agent output.
func (e *Engine) appendRunLog(ctx context.Context, runID string, kind domain.LogKind, text string) {
	run, err := e.d.Store.GetRun(ctx, runID)
	if err != nil {
		e.d.Logger.Error("store run log: read run", "run", runID, "err", err)
		return
	}
	e.storeRunLogs(ctx, run, []domain.LogLine{{RunID: runID, Time: e.d.Now(), Kind: kind, Text: text}})
}
