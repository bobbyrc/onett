package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// Limits on the text GET /problems shows. The page is for reading at a glance, so titles and
// details are cut; the stored block detail keeps more, so a person can read the whole error on
// the task.
const (
	problemTitleLimit  = 200  // characters of a title
	problemDetailLimit = 300  // characters of a detail, over all its lines
	problemTaskLimit   = 50   // tasks listed per problem; Count has the real number
	blockedDetailLimit = 2000 // characters of the underlying error kept on a task
)

// lastRunes returns s cut to n runes from its end, with a leading "…" when it is cut. The end of
// an error says what failed last, which is usually the useful part.
func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-(n-1):])
}

// projectEnv is the environment whose secret values are redacted from text about project.
func (e *Engine) projectEnv(project string) []string {
	if p := e.Config().Projects[project]; p != nil {
		return evidenceEnv(p)
	}
	return os.Environ()
}

// blockedDetail redacts and bounds the underlying error of a block before it is stored, so a
// secret in a git or CLI error never reaches the task row. Empty when there is no error.
func (e *Engine) blockedDetail(project, detail string) string {
	if strings.TrimSpace(detail) == "" {
		return ""
	}
	return lastRunes(strings.TrimSpace(redactSecrets(detail, e.projectEnv(project))), blockedDetailLimit)
}

// problemLine makes s one line that is safe to show: secrets redacted, whitespace collapsed,
// then cut to limit, from the end when keepEnd and from the start otherwise.
func problemLine(s string, env []string, limit int, keepEnd bool) string {
	x := strings.Join(strings.Fields(redactSecrets(s, env)), " ")
	if keepEnd {
		return lastRunes(x, limit)
	}
	return firstRunes(x, limit)
}

// problemDetail shapes an underlying error for a problem row. An error from a command says
// what failed on its first line and often ends with advice, so the row gets the first
// non-empty line and the last two (every line when there are at most three), each redacted and
// collapsed to single spaces. The lines share problemDetailLimit: each is cut to its share,
// keeping its end, so the whole is bounded whatever the error was.
func problemDetail(s string, env []string) string {
	var lines []string
	for _, l := range strings.Split(redactSecrets(s, env), "\n") {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > 3 {
		lines = []string{lines[0], lines[len(lines)-2], lines[len(lines)-1]}
	}
	for i, l := range lines {
		lines[i] = lastRunes(l, problemDetailLimit/len(lines))
	}
	return strings.Join(lines, "\n")
}

// heartbeatTold maps "kind:entity" to when the heartbeat last escalated that item, from the
// heartbeat's cursor. Items escalated before the time was kept have none, so they are left out.
func heartbeatTold(cursor string) map[string]time.Time {
	out := map[string]time.Time{}
	for key, mem := range loadHeartbeatState(cursor).Items {
		if !mem.Escalated || mem.EscalatedAt.IsZero() {
			continue
		}
		kind, _, _ := strings.Cut(key, ":")
		k := kind + ":" + mem.EntityID
		if at, ok := out[k]; !ok || mem.EscalatedAt.After(at) {
			out[k] = mem.EscalatedAt
		}
	}
	return out
}

// Problems lists what is wrong in every configured project: blocked tasks grouped by cause,
// failing loops and blocked goals. It reads the stores only and changes nothing.
func (e *Engine) Problems(ctx context.Context) ([]api.Problem, error) {
	cfg := e.Config()
	states, err := e.d.Store.ListLoopStates(ctx)
	if err != nil {
		return nil, fmt.Errorf("problems: list loop states: %w", err)
	}
	byKey := make(map[string]domain.LoopState, len(states))
	for _, s := range states {
		byKey[s.Name+"/"+s.Project] = s
	}
	out := []api.Problem{}
	for _, name := range cfg.ProjectNames() {
		p := cfg.Projects[name]
		env := evidenceEnv(p)
		told := heartbeatTold(byKey[loopHeartbeat+"/"+name].Cursor)
		blocked, err := e.blockedProblems(ctx, name, env, told)
		if err != nil {
			return nil, err
		}
		out = append(out, blocked...)
		out = append(out, loopProblems(cfg, name, env, byKey, told)...)
		goals, err := e.goalProblems(ctx, name, env, told)
		if err != nil {
			return nil, err
		}
		out = append(out, goals...)
	}
	utcProblems(out)
	return out, nil
}

// blockedProblems groups the blocked tasks of project by their cause, or by their whole reason
// when the block recorded no cause, so tasks that share one trouble are one problem.
func (e *Engine) blockedProblems(ctx context.Context, project string, env []string, told map[string]time.Time) ([]api.Problem, error) {
	tasks, err := e.d.Store.ListTasks(ctx, store.TaskFilter{Project: project, Statuses: []domain.TaskStatus{domain.TaskBlocked}, Limit: hbScanLimit})
	if err != nil {
		return nil, fmt.Errorf("problems: list tasks: %w", err)
	}
	groups := map[string][]domain.Task{}
	for _, t := range tasks {
		text := t.BlockedCause
		if text == "" {
			text = t.BlockedReason
		}
		text = strings.Join(strings.Fields(text), " ")
		groups[text] = append(groups[text], t)
	}
	blockedAt := func(t domain.Task) time.Time {
		if t.BlockedAt != nil {
			return *t.BlockedAt
		}
		return t.UpdatedAt
	}
	var out []api.Problem
	for text, g := range groups {
		sort.Slice(g, func(i, j int) bool {
			if !blockedAt(g[i]).Equal(blockedAt(g[j])) {
				return blockedAt(g[i]).After(blockedAt(g[j]))
			}
			return g[i].ID < g[j].ID
		})
		first, last := g[0], g[len(g)-1]
		latest := blockedAt(first)
		p := api.Problem{
			Kind:    api.ProblemBlockedTasks,
			Project: project,
			Key:     "blocked_tasks:" + project + ":" + reasonKey(text),
			Title:   problemLine(text, env, problemTitleLimit, false),
			Detail:  problemDetail(first.BlockedDetail, env),
			Count:   len(g),
			Since:   blockedAt(last),
			LastAt:  &latest,
		}
		for _, t := range g {
			if at, ok := told[hbBlockedTask+":"+t.ID]; ok && (p.ToldAt == nil || at.Before(*p.ToldAt)) {
				p.ToldAt = &at
			}
		}
		for _, t := range g[:min(len(g), problemTaskLimit)] {
			p.Tasks = append(p.Tasks, api.ProblemTask{
				ID:          t.ID,
				Title:       problemLine(t.Title, env, problemTitleLimit, false),
				Attempts:    t.Attempts,
				MaxAttempts: t.MaxAttempts,
			})
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if !out[i].Since.Equal(out[j].Since) {
			return out[i].Since.Before(out[j].Since)
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

// loopProblems reports the enabled loops of project that failed on their last pass, as one
// problem. A loop's failure streak comes from its loop_alert row when there is one.
func loopProblems(cfg *config.Config, project string, env []string, byKey map[string]domain.LoopState, told map[string]time.Time) []api.Problem {
	type failing struct {
		name   string
		st     domain.LoopState
		streak loopStreak
	}
	var fs []failing
	for _, l := range loopNames {
		if !loopOn(cfg, l, project) {
			continue
		}
		st, ok := byKey[l+"/"+project]
		if !ok || st.LastError == "" {
			continue
		}
		streak := loopStreak{Fails: 1, Since: st.LastRunAt}
		var s loopStreak
		if json.Unmarshal([]byte(byKey[loopAlertState+l+"/"+project].Cursor), &s) == nil && s.Fails > 0 && !s.Since.IsZero() {
			streak = s
		}
		fs = append(fs, failing{name: l, st: st, streak: streak})
	}
	if len(fs) == 0 {
		return nil
	}
	p := api.Problem{Kind: api.ProblemFailingLoops, Project: project, Key: "failing_loops:" + project}
	var latest *failing
	for i := range fs {
		f := &fs[i]
		if f.streak.Fails > p.Count {
			p.Count = f.streak.Fails
		}
		if p.Since.IsZero() || f.streak.Since.Before(p.Since) {
			p.Since = f.streak.Since
		}
		if latest == nil || f.st.LastRunAt.After(latest.st.LastRunAt) {
			latest = f
		}
		if !f.streak.Notified.IsZero() && (p.ToldAt == nil || f.streak.Notified.After(*p.ToldAt)) {
			at := f.streak.Notified
			p.ToldAt = &at
		}
		if at, ok := told[hbLoopError+":"+f.name]; ok && (p.ToldAt == nil || at.After(*p.ToldAt)) {
			p.ToldAt = &at
		}
		p.Loops = append(p.Loops, api.ProblemLoop{Name: f.name, Fails: f.streak.Fails, Since: f.streak.Since})
	}
	lastRun := latest.st.LastRunAt
	p.LastAt = &lastRun
	p.Detail = problemDetail(latest.st.LastError, env)
	return []api.Problem{p}
}

// goalProblems reports the blocked goals of project, oldest first.
func (e *Engine) goalProblems(ctx context.Context, project string, env []string, told map[string]time.Time) ([]api.Problem, error) {
	goals, err := e.d.Store.ListGoals(ctx, store.GoalFilter{Project: project, Statuses: []domain.GoalStatus{domain.GoalBlocked}})
	if err != nil {
		return nil, fmt.Errorf("problems: list goals: %w", err)
	}
	sort.Slice(goals, func(i, j int) bool {
		if !goals[i].UpdatedAt.Equal(goals[j].UpdatedAt) {
			return goals[i].UpdatedAt.Before(goals[j].UpdatedAt)
		}
		return goals[i].ID < goals[j].ID
	})
	var out []api.Problem
	for _, g := range goals {
		p := api.Problem{
			Kind:    api.ProblemBlockedGoal,
			Project: project,
			Key:     "blocked_goal:" + g.ID,
			Title:   problemLine(g.Title, env, problemTitleLimit, false),
			Detail:  problemLine(g.Summary, env, problemDetailLimit, true),
			Count:   1,
			Since:   g.UpdatedAt,
			GoalID:  g.ID,
		}
		if at, ok := told[hbBlockedGoal+":"+g.ID]; ok {
			p.ToldAt = &at
		}
		out = append(out, p)
	}
	return out, nil
}

// utcProblems puts every time in the problems in UTC, as the API promises.
func utcProblems(ps []api.Problem) {
	for i := range ps {
		p := &ps[i]
		p.Since = p.Since.UTC()
		if p.LastAt != nil {
			t := p.LastAt.UTC()
			p.LastAt = &t
		}
		if p.ToldAt != nil {
			t := p.ToldAt.UTC()
			p.ToldAt = &t
		}
		for j := range p.Loops {
			p.Loops[j].Since = p.Loops[j].Since.UTC()
		}
	}
}
