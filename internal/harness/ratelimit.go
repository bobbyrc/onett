package harness

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RateLimit says a run stopped because the harness's subscription or provider limit was hit, not
// because the work failed. The engine holds new runs for that harness and re-queues the task
// without counting the attempt.
type RateLimit struct {
	// ResetAt is when the limit lifts; zero when the harness gave no reset time.
	ResetAt time.Time
	// Message is the harness's own words, for the task's event log.
	Message string
}

var (
	limitText = regexp.MustCompile(`(?i)usage limit|hit your limit|limit reached|rate[ _-]?limit|too many requests|\b429\b`)
	// epochAfterPipe matches Claude Code's "Claude AI usage limit reached|1730000000".
	epochAfterPipe = regexp.MustCompile(`\|\s*(\d{10})\b`)
	// retryIn matches "try again in 2 hours 5 minutes" and "try again in 37m".
	retryIn   = regexp.MustCompile(`(?i)(?:try again|retry|resets?) in ((?:\d+\s*[a-z]+[,\s]*(?:and\s+)?)+)`)
	durPart   = regexp.MustCompile(`(?i)(\d+)\s*(days?|d|hours?|hrs?|h|minutes?|mins?|m|seconds?|secs?|s)\b`)
	clockTime = regexp.MustCompile(`(?i)(?:resets?|try again)(?: at)?\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\b`)
)

// LooksRateLimited reports whether an error text reads like a subscription or provider limit.
// Adapters apply it only to text that ended a run in error, never to an agent's normal output.
func LooksRateLimited(text string) bool { return limitText.MatchString(text) }

// ResetFromText finds the reset time in a limit message, or returns the zero time. It reads an
// epoch after a pipe ("...reached|1730000000"), a wait ("try again in 2 hours 5 minutes") and
// a clock time ("resets 3pm"). A clock time is the next such time in now's location. Times that
// are not in the future are ignored.
func ResetFromText(text string, now time.Time) time.Time {
	var at time.Time
	switch {
	case epochAfterPipe.MatchString(text):
		sec, _ := strconv.ParseInt(epochAfterPipe.FindStringSubmatch(text)[1], 10, 64)
		at = time.Unix(sec, 0)
	case retryIn.MatchString(text):
		var d time.Duration
		for _, m := range durPart.FindAllStringSubmatch(retryIn.FindStringSubmatch(text)[1], -1) {
			n, _ := strconv.Atoi(m[1])
			switch strings.ToLower(m[2])[0] {
			case 'd':
				d += time.Duration(n) * 24 * time.Hour
			case 'h':
				d += time.Duration(n) * time.Hour
			case 'm':
				d += time.Duration(n) * time.Minute
			case 's':
				d += time.Duration(n) * time.Second
			}
		}
		if d > 0 {
			at = now.Add(d)
		}
	case clockTime.MatchString(text):
		m := clockTime.FindStringSubmatch(text)
		hour, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		switch strings.ToLower(m[3]) {
		case "pm":
			if hour < 12 {
				hour += 12
			}
		case "am":
			if hour == 12 {
				hour = 0
			}
		}
		if hour > 23 || min > 59 {
			return time.Time{}
		}
		at = time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
		if !at.After(now) {
			at = at.AddDate(0, 0, 1)
		}
	}
	if !at.After(now) {
		return time.Time{}
	}
	return at
}
