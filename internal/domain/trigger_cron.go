package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronSchedule is a parsed five-field cron expression: minute, hour, day of month, month, day
// of week. Each field is a bit set: bit n is set when value n is allowed.
type CronSchedule struct {
	minute, hour, dom, month, dow uint64
	// domStar and dowStar record that the day-of-month or day-of-week field began with "*",
	// which decides how the two day fields combine (see dayMatches).
	domStar, dowStar bool
}

// cronSearchDays is five years of days.
const cronSearchDays = 5 * 366

// cronField describes one of the five fields: its name in errors and its allowed range.
type cronField struct {
	name     string
	min, max int
}

var cronFields = [5]cronField{
	{"minute", 0, 59},
	{"hour", 0, 23},
	{"day of month", 1, 31},
	{"month", 1, 12},
	{"day of week", 0, 7},
}

// ParseCron parses a five-field cron expression.
func ParseCron(expr string) (CronSchedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return CronSchedule{}, fmt.Errorf("cron: want 5 fields (minute hour day-of-month month day-of-week), got %d", len(fields))
	}
	var bits [5]uint64
	for i, f := range fields {
		b, err := parseCronField(f, cronFields[i])
		if err != nil {
			return CronSchedule{}, fmt.Errorf("cron: %s: %w", cronFields[i].name, err)
		}
		bits[i] = b
	}
	// Value 7 is Sunday too: fold it onto bit 0.
	if bits[4]&(1<<7) != 0 {
		bits[4] = bits[4]&^(1<<7) | 1
	}
	return CronSchedule{
		minute:  bits[0],
		hour:    bits[1],
		dom:     bits[2],
		month:   bits[3],
		dow:     bits[4],
		domStar: strings.HasPrefix(fields[2], "*"),
		dowStar: strings.HasPrefix(fields[4], "*"),
	}, nil
}

// parseCronField returns the bit set for one field: a comma-separated list of items.
func parseCronField(field string, spec cronField) (uint64, error) {
	var bits uint64
	for _, item := range strings.Split(field, ",") {
		if item == "" {
			return 0, fmt.Errorf("empty list item in %q", field)
		}
		base, step := item, 1
		if before, stepText, found := strings.Cut(item, "/"); found {
			base = before
			n, err := parseCronNumber(stepText, 1, 1<<30)
			if err != nil {
				return 0, fmt.Errorf("step: %w", err)
			}
			step = n
			if base != "*" && !strings.Contains(base, "-") {
				return 0, fmt.Errorf("step %q needs * or a range before it", item)
			}
		}
		low, high := spec.min, spec.max
		switch {
		case base == "*":
		case strings.Contains(base, "-"):
			a, b, _ := strings.Cut(base, "-")
			var err error
			if low, err = parseCronNumber(a, spec.min, spec.max); err != nil {
				return 0, err
			}
			if high, err = parseCronNumber(b, spec.min, spec.max); err != nil {
				return 0, err
			}
			if low > high {
				return 0, fmt.Errorf("range %q runs backwards", base)
			}
		default:
			n, err := parseCronNumber(base, spec.min, spec.max)
			if err != nil {
				return 0, err
			}
			low, high = n, n
		}
		for v := low; v <= high; v += step {
			bits |= 1 << v
		}
	}
	return bits, nil
}

// parseCronNumber reads a decimal number in [min, max]. It rejects signs and spaces, which
// strconv would otherwise accept.
func parseCronNumber(s string, min, max int) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("missing number")
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("%q is not a number", s)
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%q is out of range", s)
	}
	if n < min || n > max {
		return 0, fmt.Errorf("%d is outside %d-%d", n, min, max)
	}
	return n, nil
}

// Next returns the first time after `after` at which the schedule fires, read on the wall clock
// of loc. It returns the zero time when the schedule does not fire in the next five years.
func (s CronSchedule) Next(after time.Time, loc *time.Location) time.Time {
	y, mo, d := after.In(loc).Date()
	for i := 0; i < cronSearchDays; i++ {
		// A UTC date carries the calendar day; time.Date normalises d+i past the month's end.
		day := time.Date(y, mo, d+i, 0, 0, 0, 0, time.UTC)
		if !s.dayMatches(day.Month(), day.Day(), day.Weekday()) {
			continue
		}
		for h := 0; h < 24; h++ {
			if s.hour&(1<<h) == 0 {
				continue
			}
			for m := 0; m < 60; m++ {
				if s.minute&(1<<m) == 0 {
					continue
				}
				if t := wallInstant(day.Year(), day.Month(), day.Day(), h, m, loc); t.After(after) {
					return t
				}
			}
		}
	}
	return time.Time{}
}

// dayMatches reports whether a calendar day is allowed by the month and the two day fields.
// When both day fields are restricted, a day matches if either one does, as in classic cron.
func (s CronSchedule) dayMatches(month time.Month, day int, weekday time.Weekday) bool {
	if s.month&(1<<int(month)) == 0 {
		return false
	}
	domOK := s.dom&(1<<day) != 0
	dowOK := s.dow&(1<<int(weekday)) != 0
	if s.domStar || s.dowStar {
		return domOK && dowOK
	}
	return domOK || dowOK
}

// wallInstant returns the moment the wall clock of loc reads the given date and time. Two cases
// need a rule, because time.Date does not promise which side of a clock change it picks:
//   - clocks went forward and the time does not exist: the moment of the jump;
//   - clocks went back and the time happens twice: the first of the two.
func wallInstant(y int, mo time.Month, d, h, m int, loc *time.Location) time.Time {
	t := time.Date(y, mo, d, h, m, 0, 0, loc)
	start, end := t.ZoneBounds()
	if t.Hour() != h || t.Minute() != m || t.Day() != d {
		// The time does not exist. Go landed just before or just after the jump, so the nearer
		// bound of its zone is the jump.
		if start.IsZero() || (!end.IsZero() && end.Sub(t) < t.Sub(start)) {
			if end.IsZero() {
				return t
			}
			return end
		}
		return start
	}
	if !start.IsZero() {
		_, off := t.Zone()
		_, before := start.Add(-time.Second).Zone()
		// before > off means the clocks went back at start. If t minus that shift is still in the
		// earlier zone, it reads the same wall time and is the first of the two.
		if before > off {
			if earlier := t.Add(-time.Duration(before-off) * time.Second); earlier.Before(start) {
				return earlier
			}
		}
	}
	return t
}

// TriggerLocation returns the time zone a trigger's schedule is read in: the named IANA zone,
// or the daemon's local zone for "".
func TriggerLocation(tz string) (*time.Location, error) {
	if tz == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("time zone %q: %w", tz, err)
	}
	return loc, nil
}
