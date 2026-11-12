package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/scm"
	"github.com/bobbyrc/onett/internal/store"
)

// hasLabel reports whether labels contains want. GitHub label names are case-insensitive.
func hasLabel(labels []string, want string) bool {
	return slices.ContainsFunc(labels, func(l string) bool { return strings.EqualFold(l, want) })
}

// triggersEvery is how often the triggers loop checks a project: a schedule fires within about
// this long after its time, and a new issue is seen within it.
const triggersEvery = 30 * time.Second

// triggersLoop fires the project's enabled triggers that are due: a schedule whose time has
// passed, and a github_issue trigger for each new issue. It also runs while the factory or the
// project is paused, so that what it skipped is recorded; fireTrigger starts nothing then.
func (e *Engine) triggersLoop(ctx context.Context, p *config.Project) error {
	all, err := e.d.Store.ListTriggers(ctx)
	if err != nil {
		return fmt.Errorf("triggers: list: %w", err)
	}
	now := e.d.Now().UTC()

	var mine []domain.Trigger
	var since *time.Time
	for _, t := range all {
		if t.Project != p.Name || !t.Enabled {
			continue
		}
		mine = append(mine, t)
		if t.Kind == domain.TriggerGitHubIssue && t.IssuesAfter != nil && (since == nil || t.IssuesAfter.Before(*since)) {
			since = t.IssuesAfter
		}
	}

	var errs []error
	// The project's issues are listed once per tick, and only when an issue trigger is enabled.
	var issues []scm.Issue
	issuesOK := false
	if since != nil {
		issues, err = e.openedIssues(ctx, p, *since)
		if err != nil {
			errs = append(errs, err)
		} else {
			issuesOK = true
		}
	}

	for _, t := range mine {
		var terr error
		switch t.Kind {
		case domain.TriggerSchedule:
			terr = e.checkSchedule(ctx, t, now)
		case domain.TriggerGitHubIssue:
			if issuesOK {
				terr = e.checkIssues(ctx, p, t, issues)
			}
		}
		if terr != nil {
			errs = append(errs, fmt.Errorf("trigger %q: %w", t.Name, terr))
		}
	}
	return errors.Join(errs...)
}

// openedIssues lists the open issues of p's GitHub repository created after since. It returns
// nothing for a project that is not a GitHub repository.
func (e *Engine) openedIssues(ctx context.Context, p *config.Project, since time.Time) ([]scm.Issue, error) {
	if !isGitHubRepo(p) {
		return nil, nil
	}
	s, err := e.d.SCMFor(p)
	if err != nil {
		return nil, fmt.Errorf("triggers: scm: %w", err)
	}
	issues, err := s.ListOpenedIssues(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("triggers: list opened issues: %w", err)
	}
	return issues, nil
}

// checkSchedule fires a schedule trigger whose time has passed, once, however many scheduled
// times were missed: after downtime there is one catch-up run and never a backlog. The schedule
// is moved forward before the firing, so a crash in between loses the run rather than repeating it.
func (e *Engine) checkSchedule(ctx context.Context, t domain.Trigger, now time.Time) error {
	if t.NextRunAt == nil {
		// An enabled schedule with no next time: arm it, and fire at its next time.
		next, err := t.NextRun(now)
		if err != nil {
			return err
		}
		updated, err := e.d.Store.UpdateTrigger(ctx, t.ID, func(x *domain.Trigger) error {
			if x.NextRunAt == nil {
				x.NextRunAt = &next
			}
			return nil
		})
		if err != nil {
			return err
		}
		e.d.Bus.Emit(ctx, domain.EvTriggerUpdated, t.Project, "", "", fmt.Sprintf("trigger %q scheduled", t.Name), updated)
		return nil
	}
	if now.Before(*t.NextRunAt) {
		return nil
	}
	due := *t.NextRunAt
	next, err := t.NextRun(now)
	if err != nil {
		return err
	}
	updated, err := e.d.Store.UpdateTrigger(ctx, t.ID, func(x *domain.Trigger) error {
		if !x.Enabled || x.NextRunAt == nil || !x.NextRunAt.Equal(due) {
			return errSkip
		}
		x.NextRunAt = &next
		return nil
	})
	if errors.Is(err, errSkip) {
		return nil
	}
	if err != nil {
		return err
	}
	e.d.Bus.Emit(ctx, domain.EvTriggerUpdated, updated.Project, "", "", fmt.Sprintf("trigger %q next runs %s", updated.Name, next.Format(time.RFC3339)), updated)
	_, err = e.fireTrigger(ctx, updated, domain.TriggerFiring{Cause: domain.FiringCauseSchedule, ScheduledFor: &due}, nil)
	return err
}

// checkIssues fires a github_issue trigger for each issue it has not handled yet. An issue is
// settled once it has a firing that started or failed: at most once, across restarts, because
// the firing row is the record. An issue skipped for a pause or the budget stays pending and is
// recorded as skipped once, not once per tick.
func (e *Engine) checkIssues(ctx context.Context, p *config.Project, t domain.Trigger, issues []scm.Issue) error {
	cfg := e.Config()
	loops := cfg.LoopsFor(p.Name)
	var errs []error
	for _, is := range issues {
		// An issue that existed before the trigger was created or last enabled.
		if t.IssuesAfter == nil || !is.CreatedAt.After(*t.IssuesAfter) {
			continue
		}
		// The trigger watches only issues with its label.
		if t.Label != "" && !hasLabel(is.Labels, t.Label) {
			continue
		}
		// The repo may be public: only people with write access may start a turn.
		if !trustedAssociation(is.AuthorAssociation) {
			continue
		}
		// An issue onett filed itself: firing on it could loop.
		if loops.Friction.Label != "" && hasLabel(is.Labels, loops.Friction.Label) {
			continue
		}
		// Issue intake already turns it into a task; intake wins.
		if loopOn(cfg, loopIssueIntake, p.Name) && hasLabel(is.Labels, loops.IssueIntake.Label) {
			continue
		}

		last, lerr := e.d.Store.LastIssueFiring(ctx, t.ID, is.Number)
		if lerr == nil && last.Outcome != domain.FiringSkipped {
			continue // settled: started or failed
		}
		if lerr != nil && !errors.Is(lerr, store.ErrNotFound) {
			errs = append(errs, fmt.Errorf("issue #%d: %w", is.Number, lerr))
			continue
		}
		if reason := e.triggerHeld(ctx, t); reason != "" && lerr == nil && last.Reason == reason {
			continue // this skip is already recorded; the issue stays pending
		}

		// The owner may have disabled, re-enabled or deleted the trigger since this tick read it
		// (listing the issues takes a while): the stored trigger decides.
		cur, err := e.d.Store.GetTrigger(ctx, t.ID)
		if errors.Is(err, store.ErrNotFound) {
			return errors.Join(errs...) // deleted: nothing more to fire
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("issue #%d: %w", is.Number, err))
			continue
		}
		if !cur.Enabled || cur.IssuesAfter == nil || !is.CreatedAt.After(*cur.IssuesAfter) {
			continue
		}

		is := is
		if _, err := e.fireTrigger(ctx, cur, domain.TriggerFiring{Cause: domain.FiringCauseGitHubIssue, IssueNumber: is.Number, IssueURL: is.URL}, &is); err != nil {
			errs = append(errs, fmt.Errorf("issue #%d: %w", is.Number, err))
		}
	}
	return errors.Join(errs...)
}
