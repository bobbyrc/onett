package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ts parses an RFC 3339 instant for a test expectation.
func ts(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return v
}

// nextCron parses expr and asserts that Next(after) in loc equals want.
func nextCron(t *testing.T, expr string, after, want time.Time, loc *time.Location) {
	t.Helper()
	s, err := ParseCron(expr)
	require.NoError(t, err)
	got := s.Next(after, loc)
	assert.True(t, want.Equal(got), "%q after %s: want %s, got %s", expr, after.UTC(), want.UTC(), got.UTC())
}

func TestCronNextKeepsWallClockAcrossDaylightSaving(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	// 8 March 2026 is the spring-forward day; 05:00 EST is 10:00 UTC before it and 09:00 UTC after.
	nextCron(t, "0 5 * * *", ts(t, "2026-03-06T12:00:00Z"), ts(t, "2026-03-07T10:00:00Z"), ny)
	nextCron(t, "0 5 * * *", ts(t, "2026-03-07T10:00:00Z"), ts(t, "2026-03-08T09:00:00Z"), ny)
}

func TestCronNextSkippedHourFiresAtTheJump(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	nextCron(t, "30 2 * * *", ts(t, "2026-03-07T12:00:00Z"), ts(t, "2026-03-08T07:00:00Z"), ny)
	nextCron(t, "30 2 * * *", ts(t, "2026-03-08T07:00:00Z"), ts(t, "2026-03-09T06:30:00Z"), ny)
	nextCron(t, "*/15 2 * * *", ts(t, "2026-03-08T06:59:00Z"), ts(t, "2026-03-08T07:00:00Z"), ny)
	nextCron(t, "*/15 2 * * *", ts(t, "2026-03-08T07:00:00Z"), ts(t, "2026-03-09T06:00:00Z"), ny)
}

func TestCronNextRepeatedHourFiresOnce(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	nextCron(t, "30 1 * * *", ts(t, "2026-10-31T12:00:00Z"), ts(t, "2026-11-01T05:30:00Z"), ny)
	nextCron(t, "30 1 * * *", ts(t, "2026-11-01T05:30:00Z"), ts(t, "2026-11-02T06:30:00Z"), ny)
	nextCron(t, "*/15 * * * *", ts(t, "2026-11-01T05:45:00Z"), ts(t, "2026-11-01T07:00:00Z"), ny)
}

func TestCronNextWeekdaysAndLists(t *testing.T) {
	nextCron(t, "30 9 * * 1-5", ts(t, "2026-11-13T10:00:00Z"), ts(t, "2026-11-16T09:30:00Z"), time.UTC)
	nextCron(t, "0 8 * * 1,4", ts(t, "2026-11-16T08:00:00Z"), ts(t, "2026-11-19T08:00:00Z"), time.UTC)
	nextCron(t, "0 0 * * 7", ts(t, "2026-11-13T00:00:00Z"), ts(t, "2026-11-15T00:00:00Z"), time.UTC)
	nextCron(t, "0 */6 * * *", ts(t, "2026-01-01T07:00:00Z"), ts(t, "2026-01-01T12:00:00Z"), time.UTC)
	nextCron(t, "*/45 * * * *", ts(t, "2026-01-01T07:00:00Z"), ts(t, "2026-01-01T07:45:00Z"), time.UTC)
	nextCron(t, "*/45 * * * *", ts(t, "2026-01-01T07:45:00Z"), ts(t, "2026-01-01T08:00:00Z"), time.UTC)
}

func TestCronDayOfMonthOrDayOfWeek(t *testing.T) {
	// Both day fields restricted: either the 13th or a Monday.
	nextCron(t, "0 0 13 * 1", ts(t, "2026-11-10T00:00:00Z"), ts(t, "2026-11-13T00:00:00Z"), time.UTC)
	nextCron(t, "0 0 13 * 1", ts(t, "2026-11-13T00:00:00Z"), ts(t, "2026-11-16T00:00:00Z"), time.UTC)
	// Day of month starts with "*", so both must match: an odd day that is a Monday.
	nextCron(t, "0 0 */2 * 1", ts(t, "2026-11-10T00:00:00Z"), ts(t, "2026-11-23T00:00:00Z"), time.UTC)
}

func TestCronNextIsAfterTheGivenTime(t *testing.T) {
	nextCron(t, "0 5 * * *", ts(t, "2026-01-01T05:00:00Z"), ts(t, "2026-01-02T05:00:00Z"), time.UTC)
}

func TestCronNeverFires(t *testing.T) {
	s, err := ParseCron("0 0 30 2 *")
	require.NoError(t, err)
	assert.True(t, s.Next(ts(t, "2026-01-01T00:00:00Z"), time.UTC).IsZero())
}

func TestParseCronRejects(t *testing.T) {
	bad := []string{
		"",
		"* * * *",
		"* * * * * *",
		"60 * * * *",
		"* 24 * * *",
		"* * 0 * *",
		"* * 32 * *",
		"* * * 0 *",
		"* * * 13 *",
		"* * * * 8",
		"*/0 * * * *",
		"5/2 * * * *",
		"a * * * *",
		"1- * * * *",
		"5-1 * * * *",
		"@daily",
		"* * * * mon",
		"1,,2 * * * *",
		"+5 * * * *",
		"*/ * * * *",
	}
	for _, expr := range bad {
		_, err := ParseCron(expr)
		if assert.Error(t, err, "expr %q", expr) {
			assert.True(t, strings.HasPrefix(err.Error(), "cron: "), "expr %q: %v", expr, err)
		}
	}
}

func TestTriggerLocation(t *testing.T) {
	loc, err := TriggerLocation("")
	require.NoError(t, err)
	assert.Equal(t, time.Local, loc)

	chi, err := TriggerLocation("America/Chicago")
	require.NoError(t, err)
	assert.Equal(t, "America/Chicago", chi.String())

	_, err = TriggerLocation("Mars/Olympus")
	assert.Error(t, err)
}
