package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bobbyrc/onett/internal/notify"
)

// A loop that keeps failing tells the owner, in plain code with no model call. Before this a
// failing loop only logged and emitted an event, and the heartbeat was the only fallback, which
// says nothing when the heartbeat itself (or the model call it makes) is what is broken.

const (
	// loopAlertAfter and loopAlertMinAge must both hold before the owner is told: this many failed
	// passes in a row, and a streak at least this old. Loops run at very different intervals, and
	// three failed passes of a 30-second loop are a 90-second blip, not a reason to alert.
	loopAlertAfter  = 3
	loopAlertMinAge = 10 * time.Minute
	// loopAlertRepeat is how often a loop that is still failing is reported again.
	loopAlertRepeat = 24 * time.Hour
)

// loopAlertState prefixes the LoopState name that keeps one loop's failure streak: the row for
// loop X of project P is {Name: "loop_alert:X", Project: P}, with a loopStreak as JSON in Cursor
// ("" when the loop is not failing). It is a row in the store, like the pause switch, so a
// restart neither forgets a streak nor repeats its notification.
const loopAlertState = "loop_alert:"

// loopStreak is a run of failed passes of one loop for one project with no success in between.
type loopStreak struct {
	Fails    int       `json:"fails"`
	Since    time.Time `json:"since"`    // when the first failure of the streak happened
	Notified time.Time `json:"notified"` // when the owner was last told; zero when not yet
}

// trackLoopStreak counts one pass of loop name for project towards its failure streak and
// notifies the owner when the streak is long and old enough. runLoopOnce calls it under the
// loop's lock after every pass. failed says whether this pass failed, wasFailing whether the pass
// before it did, and msg is this pass's error, already redacted. Any error text counts: a text
// that changes every pass (a request ID, a timestamp) must not hide a loop that never succeeds.
func (e *Engine) trackLoopStreak(ctx context.Context, name, project, msg string, failed, wasFailing bool) {
	if !failed && !wasFailing {
		return // the usual pass: there is no streak to end, so the store is not touched
	}
	row, err := e.d.Store.GetLoopState(ctx, loopAlertState+name, project)
	if err != nil {
		e.d.Logger.Error("loop streak", "loop", name, "project", project, "err", err)
		return
	}
	if !failed {
		if row.Cursor == "" {
			return
		}
		row.Cursor = ""
	} else {
		now := e.d.Now().UTC()
		var s loopStreak
		// A cursor that is empty or does not parse starts a new streak.
		if json.Unmarshal([]byte(row.Cursor), &s) != nil || s.Fails <= 0 || s.Since.IsZero() {
			s = loopStreak{Since: now}
		}
		s.Fails++
		due := s.Notified.IsZero() || now.Sub(s.Notified) >= loopAlertRepeat
		if s.Fails >= loopAlertAfter && now.Sub(s.Since) >= loopAlertMinAge && due {
			s.Notified = now
			e.notify(ctx, notify.PriorityHigh, fmt.Sprintf("Loop %s keeps failing (%s)", name, project),
				fmt.Sprintf("Failed %d times in a row since %s.\n%s", s.Fails, s.Since.Format("2006-01-02 15:04 UTC"), oneLine(msg, nil)),
				"/loops")
		}
		raw, err := json.Marshal(s)
		if err != nil {
			e.d.Logger.Error("loop streak", "loop", name, "project", project, "err", err)
			return
		}
		row.Cursor = string(raw)
	}
	if err := e.d.Store.SaveLoopState(ctx, row); err != nil {
		e.d.Logger.Error("save loop streak", "loop", name, "project", project, "err", err)
	}
}
