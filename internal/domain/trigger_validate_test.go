package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validScheduleTrigger() Trigger {
	return Trigger{
		Name:     "Morning issues",
		Kind:     TriggerSchedule,
		Project:  "p",
		Prompt:   "do it",
		Cron:     "0 5 * * *",
		TimeZone: "America/Chicago",
	}
}

func validIssueTrigger() Trigger {
	return Trigger{
		Name:    "Triage",
		Kind:    TriggerGitHubIssue,
		Project: "p",
		Prompt:  "look",
		Label:   "bug",
	}
}

func TestValidateAcceptsValidTriggers(t *testing.T) {
	now := ts(t, "2026-11-12T00:00:00Z")
	assert.NoError(t, validScheduleTrigger().Validate(now))
	assert.NoError(t, validIssueTrigger().Validate(now))
}

func TestValidateRejects(t *testing.T) {
	now := ts(t, "2026-11-12T00:00:00Z")
	cases := []struct {
		name   string
		change func(*Trigger)
		want   string
	}{
		{"empty name", func(tr *Trigger) { tr.Name = "  " }, "name is required"},
		{"name of 101 runes", func(tr *Trigger) { tr.Name = strings.Repeat("é", 101) }, "name is longer than 100 characters"},
		{"whitespace-only prompt", func(tr *Trigger) { tr.Prompt = " \n\t" }, "prompt is required"},
		{"prompt of 20001 runes", func(tr *Trigger) { tr.Prompt = strings.Repeat("é", 20001) }, "prompt is longer than 20000 characters"},
		{"empty project", func(tr *Trigger) { tr.Project = "" }, "project is required"},
		{"schedule with a label", func(tr *Trigger) { tr.Label = "bug" }, "label belongs to a github_issue trigger"},
		{"schedule with empty cron", func(tr *Trigger) { tr.Cron = "" }, "cron is required for a schedule trigger"},
		{"schedule that never fires", func(tr *Trigger) { tr.Cron = "0 0 30 2 *" }, "never fires"},
		{"schedule with bad minute", func(tr *Trigger) { tr.Cron = "61 * * * *" }, "cron: minute"},
		{"schedule with unknown zone", func(tr *Trigger) { tr.TimeZone = "Mars/Olympus" }, "time zone"},
		{"issue trigger with a cron", func(tr *Trigger) { tr.Kind = TriggerGitHubIssue; tr.Label = ""; tr.TimeZone = "" }, "cron belongs to a schedule trigger"},
		{"issue trigger with a time zone", func(tr *Trigger) { tr.Kind = TriggerGitHubIssue; tr.Label = ""; tr.Cron = ""; tr.TimeZone = "UTC" }, "time_zone belongs to a schedule trigger"},
		{"unknown kind", func(tr *Trigger) { tr.Kind = "webhook" }, "kind must be"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := validScheduleTrigger()
			c.change(&tr)
			assert.ErrorContains(t, tr.Validate(now), c.want)
		})
	}
}

func TestValidateAcceptsLimitLengths(t *testing.T) {
	now := ts(t, "2026-11-12T00:00:00Z")
	tr := validScheduleTrigger()
	tr.Name = strings.Repeat("é", 100)
	tr.Prompt = strings.Repeat("é", 20000)
	assert.NoError(t, tr.Validate(now))
}

func TestNextRun(t *testing.T) {
	tr := validScheduleTrigger()
	next, err := tr.NextRun(ts(t, "2026-11-12T00:00:00Z"))
	require.NoError(t, err)
	assert.True(t, ts(t, "2026-11-12T11:00:00Z").Equal(next), "got %s", next.UTC())
}
