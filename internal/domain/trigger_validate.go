package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// NextRun returns the first time after `after` at which a schedule trigger fires. It is an
// error when the cron expression or the time zone is invalid, or when the schedule never fires
// in the next five years.
func (t Trigger) NextRun(after time.Time) (time.Time, error) {
	s, err := ParseCron(t.Cron)
	if err != nil {
		return time.Time{}, err
	}
	loc, err := TriggerLocation(t.TimeZone)
	if err != nil {
		return time.Time{}, err
	}
	next := s.Next(after, loc)
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("cron: %q never fires in the next five years", t.Cron)
	}
	return next.UTC(), nil
}

// Validate checks the owner-written parts of a trigger: the name, the prompt, the kind and the
// fields that belong to the kind. It does not know which projects exist. now is used to check
// that a schedule fires at all.
func (t Trigger) Validate(now time.Time) error {
	if strings.TrimSpace(t.Name) == "" {
		return errors.New("name is required")
	}
	if utf8.RuneCountInString(t.Name) > TriggerNameMax {
		return fmt.Errorf("name is longer than %d characters", TriggerNameMax)
	}
	if strings.TrimSpace(t.Prompt) == "" {
		return errors.New("prompt is required")
	}
	if utf8.RuneCountInString(t.Prompt) > TriggerPromptMax {
		return fmt.Errorf("prompt is longer than %d characters", TriggerPromptMax)
	}
	if t.Project == "" {
		return errors.New("project is required")
	}
	switch t.Kind {
	case TriggerSchedule:
		if t.Label != "" {
			return errors.New("label belongs to a github_issue trigger")
		}
		if t.Cron == "" {
			return errors.New("cron is required for a schedule trigger")
		}
		if _, err := t.NextRun(now); err != nil {
			return err
		}
	case TriggerGitHubIssue:
		if t.Cron != "" {
			return errors.New("cron belongs to a schedule trigger")
		}
		if t.TimeZone != "" {
			return errors.New("time_zone belongs to a schedule trigger")
		}
	default:
		return fmt.Errorf("kind must be %q or %q", TriggerSchedule, TriggerGitHubIssue)
	}
	return nil
}
