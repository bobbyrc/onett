package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/scm"
)

// frictionMatchThreshold is the Jaccard similarity of two titles' token sets at which they count
// as the same problem.
const frictionMatchThreshold = 0.5

// frictionTextThreshold is the Jaccard similarity of two reports' whole texts (title and body) at
// which they count as the same problem. On the 84 friction issues filed up to 2026-11-06 it merged
// no two different problems; lower values did.
const frictionTextThreshold = 0.25

// frictionCommentCap is how many "Reported again" comments onett posts on one issue. Later reports
// of the problem are still stored and linked, so a hot problem cannot flood the issue.
const frictionCommentCap = 10

// frictionAgainPrefix starts the comment that adds a repeat report to an issue.
const frictionAgainPrefix = "Reported again by run `"

// frictionFooterMark starts the footer fileFriction appends to the body of an issue it opens.
const frictionFooterMark = "\n\n---\nReported by onett agents "

// frictionDailyIssueCap is how many new issues the friction loop opens for one project in one
// day, so a misbehaving agent cannot flood the tracker. Reports over it stay stored and are filed
// on a later day.
const frictionDailyIssueCap = 20

// frictionFiledState names the LoopState whose cursor counts the issues opened today, as
// "<local date>:<count>".
const frictionFiledState = "friction_filed"

// errFrictionCap is returned by fileFriction when f needs a new issue and the day's are used up.
var errFrictionCap = errors.New("daily friction issue cap reached")

// frictionPass is what one pass of the loop carries from report to report.
type frictionPass struct {
	linked map[string]int // reports linked to each issue, by issue URL
	filed  int            // issues opened for the project today
}

// frictionStopWords are dropped from titles before comparing them.
var frictionStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true, "to": true, "of": true, "in": true,
	"on": true, "for": true, "with": true, "and": true, "or": true, "by": true, "at": true,
	"from": true, "it": true, "this": true, "that": true, "be": true, "not": true, "no": true,
	"so": true, "as": true,
}

// frictionSuffixes are stripped by frictionStem, in order.
var frictionSuffixes = []string{"ing", "ed", "er", "es", "s"}

// frictionTokens turns a text into the set of its significant stems: lowercase words split on
// anything that is not a letter or digit, without stop words and words shorter than three
// characters.
func frictionTokens(title string) map[string]bool {
	out := map[string]bool{}
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, w := range words {
		if utf8.RuneCountInString(w) < 3 || frictionStopWords[w] {
			continue
		}
		out[frictionStem(w)] = true
	}
	return out
}

// frictionStem strips the suffixes in frictionSuffixes, repeatedly, while at least four characters
// remain. It is a crude stemmer: "listening" and "listeners" both become "listen".
func frictionStem(w string) string {
	for stripped := true; stripped; {
		stripped = false
		for _, suf := range frictionSuffixes {
			if strings.HasSuffix(w, suf) && len(w)-len(suf) >= 4 {
				w = w[:len(w)-len(suf)]
				stripped = true
				break
			}
		}
	}
	return w
}

// frictionSimilarity is the Jaccard similarity of two token sets: the size of their intersection
// over the size of their union. Two empty sets are not similar.
func frictionSimilarity(a, b map[string]bool) float64 {
	shared := 0
	for t := range a {
		if b[t] {
			shared++
		}
	}
	union := len(a) + len(b) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

// mostSimilar returns the index of the candidate most similar to text, when that similarity is at
// least threshold. On a tie the earlier candidate wins.
func mostSimilar(text string, candidates []string, threshold float64) (int, bool) {
	want := frictionTokens(text)
	best, bestScore := -1, 0.0
	for i, c := range candidates {
		if s := frictionSimilarity(want, frictionTokens(c)); s >= threshold && s > bestScore {
			best, bestScore = i, s
		}
	}
	return best, best >= 0
}

// similarTitle returns the index of the candidate most similar to title, when that similarity is at
// least frictionMatchThreshold. On a tie the earlier candidate wins.
func similarTitle(title string, candidates []string) (int, bool) {
	return mostSimilar(title, candidates, frictionMatchThreshold)
}

// frictionText is what two reports are compared on: the title and the body, without the footer
// onett adds to the body of an issue it opens.
func frictionText(title, body string) string {
	body, _, _ = strings.Cut(body, frictionFooterMark)
	return title + "\n\n" + body
}

// similarIssue returns the index of the open issue that reports the same problem as f: the one
// with the most similar title or, failing that, the one with the most similar title and body.
func similarIssue(f domain.Friction, open []scm.Issue) (int, bool) {
	titles := make([]string, len(open))
	texts := make([]string, len(open))
	for i, is := range open {
		titles[i] = is.Title
		texts[i] = frictionText(is.Title, is.Body)
	}
	if i, ok := similarTitle(f.Title, titles); ok {
		return i, true
	}
	return mostSimilar(frictionText(f.Title, f.Body), texts, frictionTextThreshold)
}

// claimedIssue returns the index in open of the issue f's agent named as the same problem. The
// claim holds only when that issue is open and a friction report of this project is already linked
// to it (linked counts reports by issue URL), so an agent can only point at an issue that came
// from friction. Otherwise the index is -1 and reason says why.
func claimedIssue(f domain.Friction, open []scm.Issue, linked map[string]int) (idx int, reason string) {
	for i, is := range open {
		if is.Number != f.Issue {
			continue
		}
		if linked[is.URL] == 0 {
			return -1, "no friction report of this project is linked to that issue"
		}
		return i, ""
	}
	return -1, "not an open friction issue"
}

// frictionComments counts the "Reported again" comments already on an issue.
func frictionComments(is scm.Issue) int {
	n := 0
	for _, c := range is.Comments {
		if strings.HasPrefix(c.Body, frictionAgainPrefix) {
			n++
		}
	}
	return n
}

// frictionLoop files a GitHub issue for each friction report that has reached its threshold and
// has no issue yet. A report whose problem already has an open issue is added to that issue
// instead (see fileFriction).
//
// It opens at most frictionDailyIssueCap new issues a day; once they are used up it stops filing
// until the next day, and the remaining reports stay stored.
func (e *Engine) frictionLoop(ctx context.Context, p *config.Project) error {
	loops := e.Config().LoopsFor(p.Name)
	if !isGitHubRepo(p) || !loops.FrictionAutoFile() {
		return nil
	}
	s, err := e.d.SCMFor(p)
	if err != nil {
		return fmt.Errorf("friction: scm: %w", err)
	}
	items, err := e.d.Store.ListFriction(ctx, p.Name, 200)
	if err != nil {
		return fmt.Errorf("friction: list: %w", err)
	}
	day := e.d.Now().Local().Format("2006-01-02")
	st, err := e.d.Store.GetLoopState(ctx, frictionFiledState, p.Name)
	if err != nil {
		return fmt.Errorf("friction: read filed count: %w", err)
	}
	// pass.linked counts the reports already linked to each issue: fileFriction checks an agent's
	// claim against it and adds each report it links.
	pass := &frictionPass{linked: map[string]int{}}
	if d, n, ok := strings.Cut(st.Cursor, ":"); ok && d == day {
		pass.filed, _ = strconv.Atoi(n)
	}
	filedBefore := pass.filed
	for _, f := range items {
		if f.IssueURL != "" {
			pass.linked[f.IssueURL]++
		}
	}
	var errs []error
	capped := 0
	for _, f := range items {
		if f.IssueURL != "" || f.Count < loops.Friction.MinCount {
			continue
		}
		if capped > 0 {
			capped++ // the day's issues are used up: leave the rest for a later day
			continue
		}
		err := e.fileFriction(ctx, s, p, f, loops, pass)
		switch {
		case errors.Is(err, errFrictionCap):
			capped = 1
		case err != nil:
			errs = append(errs, fmt.Errorf("friction %s: %w", f.ID, err))
		}
	}
	if pass.filed != filedBefore {
		st.Cursor = day + ":" + strconv.Itoa(pass.filed)
		if err := e.d.Store.SaveLoopState(ctx, st); err != nil {
			errs = append(errs, fmt.Errorf("friction: save filed count: %w", err))
		}
	}
	if capped > 0 {
		e.d.Logger.Warn("friction: daily issue cap reached; reports stay stored and unfiled",
			"project", p.Name, "cap", frictionDailyIssueCap, "unfiled", capped)
	}
	if err := e.refreshFrictionStates(ctx, s, loops, items); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// refreshFrictionStates copies each filed issue's current state ("open" or "closed") onto its
// friction report, so the UI can tell resolved problems from open ones.
func (e *Engine) refreshFrictionStates(ctx context.Context, s scm.SCM, loops config.Loops, items []domain.Friction) error {
	issues, err := s.ListIssues(ctx, loops.Friction.Label, "all")
	if err != nil {
		return fmt.Errorf("friction: list issue states: %w", err)
	}
	states := make(map[string]string, len(issues))
	for _, is := range issues {
		states[is.URL] = is.State
	}
	var errs []error
	for _, f := range items {
		st, ok := states[f.IssueURL]
		if f.IssueURL == "" || !ok || st == "" || st == f.IssueState {
			continue
		}
		if _, err := e.d.Store.UpdateFriction(ctx, f.ID, func(x *domain.Friction) error {
			x.IssueState = st
			return nil
		}); err != nil {
			errs = append(errs, fmt.Errorf("friction %s: set state: %w", f.ID, err))
		}
	}
	return errors.Join(errs...)
}

// fileFriction links friction f to an existing issue or creates one, and records the issue URL
// and state on f. It looks for an existing issue in three steps: one with the same title; the open
// issue the reporting agent named, when that claim holds (see claimedIssue); the open issue whose
// words are most similar (see similarIssue). A report matched in the last two steps is posted on
// the issue as a comment, up to frictionCommentCap comments. Nothing here closes or relabels an
// issue, and the report's row is always kept. It returns errFrictionCap, changing nothing, when f
// needs a new issue and the day's frictionDailyIssueCap are used up.
func (e *Engine) fileFriction(ctx context.Context, s scm.SCM, p *config.Project, f domain.Friction, loops config.Loops, pass *frictionPass) error {
	// The text goes to a tracker that may be public, so secrets are redacted first. Everything
	// below, including the search and the comparison with open issues, uses the redacted text.
	env := evidenceEnv(p)
	f.Title, f.Body = redactSecrets(f.Title, env), redactSecrets(f.Body, env)
	matches, err := s.SearchIssues(ctx, f.Title)
	if err != nil {
		return fmt.Errorf("search issues: %w", err)
	}
	url, state := "", ""
	for _, m := range matches {
		if strings.EqualFold(m.Title, f.Title) {
			url, state = m.URL, m.State
			break
		}
	}
	if url == "" {
		open, err := s.ListIssues(ctx, loops.Friction.Label, "open")
		if err != nil {
			return fmt.Errorf("list open friction issues: %w", err)
		}
		i, ok := -1, false
		if f.Issue != 0 {
			var reason string
			if i, reason = claimedIssue(f, open, pass.linked); reason != "" {
				e.d.Logger.Warn("friction: claim rejected", "friction", f.ID, "issue", f.Issue, "reason", reason)
			}
			ok = i >= 0
		}
		if !ok {
			i, ok = similarIssue(f, open)
		}
		if ok {
			if frictionComments(open[i]) < frictionCommentCap {
				msg := fmt.Sprintf("%s%s`: %s\n\n%s", frictionAgainPrefix, f.RunID, f.Title, f.Body)
				if err := s.CommentIssue(ctx, open[i].Number, msg); err != nil {
					return fmt.Errorf("comment on issue #%d: %w", open[i].Number, err)
				}
			}
			url, state = open[i].URL, open[i].State
		}
	}
	if url == "" {
		if pass.filed >= frictionDailyIssueCap {
			return errFrictionCap
		}
		labels := []string{loops.Friction.Label}
		if loops.Friction.AutoQueue {
			labels = append(labels, loops.IssueIntake.Label)
		}
		body := f.Body + frictionFooterMark + fmt.Sprintf("%d time(s); first in run `%s`.", f.Count, f.RunID)
		issue, err := s.CreateIssue(ctx, scm.IssueSpec{Title: f.Title, Body: body, Labels: labels})
		if err != nil {
			return fmt.Errorf("create issue: %w", err)
		}
		pass.filed++
		url, state = issue.URL, issue.State
		e.d.Bus.Emit(ctx, domain.EvIssueFiled, p.Name, "", "",
			fmt.Sprintf("filed issue #%d: %s", issue.Number, f.Title),
			map[string]any{"number": issue.Number, "url": issue.URL, "title": f.Title})
	}
	if state == "" {
		state = "open"
	}
	if _, err := e.d.Store.UpdateFriction(ctx, f.ID, func(x *domain.Friction) error {
		x.IssueURL = url
		x.IssueState = state
		return nil
	}); err != nil {
		return err
	}
	pass.linked[url]++
	return nil
}

// frictionFingerprintFor returns the fingerprint that a new friction report with this title should
// be stored under. When the project already has a similar report, that report's fingerprint is
// reused so the count goes up instead of a near-duplicate row being added.
func (e *Engine) frictionFingerprintFor(ctx context.Context, project, title string) string {
	existing, err := e.d.Store.ListFriction(ctx, project, 200)
	if err != nil {
		e.d.Logger.Warn("friction dedupe: list", "project", project, "err", err)
		return frictionFingerprint(title)
	}
	titles := make([]string, len(existing))
	for i, f := range existing {
		titles[i] = f.Title
	}
	if i, ok := similarTitle(title, titles); ok {
		return existing[i].Fingerprint
	}
	return frictionFingerprint(title)
}
