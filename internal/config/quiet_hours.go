package config

import (
	"fmt"
	"strings"
	"time"
)

// QuietHours is a daily window of local time, such as 22:00-07:00. A window whose end is
// before its start wraps midnight. The zero value is no window: it contains no time.
type QuietHours struct {
	start, end int // minutes since local midnight
	set        bool
}

// ParseQuietHours reads "HH:MM-HH:MM" (24-hour clock). An empty string is no window.
func ParseQuietHours(s string) (QuietHours, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return QuietHours{}, nil
	}
	from, to, ok := strings.Cut(s, "-")
	if !ok {
		return QuietHours{}, fmt.Errorf("want HH:MM-HH:MM, not %q", s)
	}
	start, err := parseClock(from)
	if err != nil {
		return QuietHours{}, err
	}
	end, err := parseClock(to)
	if err != nil {
		return QuietHours{}, err
	}
	if start == end {
		return QuietHours{}, fmt.Errorf("start and end are both %q: the window would be empty or all day", strings.TrimSpace(from))
	}
	return QuietHours{start: start, end: end, set: true}, nil
}

func parseClock(s string) (int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("want HH:MM (24-hour clock), not %q", strings.TrimSpace(s))
	}
	return t.Hour()*60 + t.Minute(), nil
}

// Contains reports whether t, read in its own location, falls inside the window. The start is
// inside and the end is outside, so 22:00-07:00 is quiet at 22:00 and awake at 07:00.
func (q QuietHours) Contains(t time.Time) bool {
	if !q.set {
		return false
	}
	m := t.Hour()*60 + t.Minute()
	if q.start < q.end {
		return m >= q.start && m < q.end
	}
	return m >= q.start || m < q.end
}

// validateHeartbeat checks the heartbeat settings found at path.
func validateHeartbeat(path, quiet string, stale time.Duration, escalateAfter int, add func(format string, args ...any)) {
	if _, err := ParseQuietHours(quiet); err != nil {
		add("%s.quiet_hours: %v", path, err)
	}
	if stale < 0 {
		add("%s.stale_queued: must be 0 (default 4h) or greater", path)
	}
	if escalateAfter < 0 {
		add("%s.escalate_after: must be 0 (default 3) or greater", path)
	}
}
