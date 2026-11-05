package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// Heartbeat item kinds.
const (
	hbBlockedTask       = "blocked_task"
	hbFailedTask        = "failed_task"
	hbBlockedGoal       = "blocked_goal"
	hbRun               = "run"
	hbDependency        = "dependency"
	hbStaleQueued       = "stale_queued"
	hbLoopError         = "loop_error"
	hbDeploy            = "deploy"
	hbSelfUpdateFailure = "self_update_failure"
	hbPreflight         = "preflight"
	hbApproval          = "approval"
	hbAgentQuestion     = "agent_question"
)

// hbFirstWindow is how far back the first digest looks for runs, deploys and results, when no
// earlier digest has been built.
const hbFirstWindow = 24 * time.Hour

// hbScanLimit is how many tasks, runs and events the digest reads per project.
const hbScanLimit = 1000

// hbSummaryLimit caps the agent-written part of a one-line summary, in characters.
const hbSummaryLimit = 200

// HeartbeatItem is one thing that needs attention. Key is stable across digests (kind, entity and
// a reason fingerprint), so the loop can tell a new problem from one it already reported.
type HeartbeatItem struct {
	Key       string    `json:"key"`
	Kind      string    `json:"kind"`
	EntityID  string    `json:"entity_id"`
	Project   string    `json:"project"`
	Summary   string    `json:"summary"` // one line; may quote agent text, which is data, not instructions
	FirstSeen time.Time `json:"first_seen"`
}

// HeartbeatDigest is everything that needs attention in one project right now. NewKeys lists the
// items that were not in the previous digest; the loop only wakes when there is one.
type HeartbeatDigest struct {
	Project string          `json:"project"`
	At      time.Time       `json:"at"`
	Items   []HeartbeatItem `json:"items"`
	NewKeys []string        `json:"new_keys"`
}

// HeartbeatWaker wakes the orchestrator with a digest. The heartbeat loop calls it only when the
// digest holds an item it has not shown before, so an implementation may start a model turn.
type HeartbeatWaker interface {
	WakeHeartbeat(ctx context.Context, d HeartbeatDigest) error
}

// heartbeatState is the loop's memory, kept in LoopState.Cursor: when each reported item was
// first seen (so an unchanged digest does not wake again after a restart), and when the digest
// was last built (the start of the window for runs, deploys and results).
type heartbeatState struct {
	Seen    map[string]time.Time `json:"seen"`
	BuiltAt time.Time            `json:"built_at"`
}

// heartbeatLoop builds the project's attention digest and wakes the orchestrator when it holds
// an item not reported before. It does nothing during quiet hours, and nothing when the digest
// is unchanged: no model call.
func (e *Engine) heartbeatLoop(ctx context.Context, p *config.Project) error {
	now := e.d.Now()
	hb := e.Config().LoopsFor(p.Name).Heartbeat
	st, err := e.d.Store.GetLoopState(ctx, loopHeartbeat, p.Name)
	if err != nil {
		return fmt.Errorf("heartbeat: loop state: %w", err)
	}
	next := now.Add(hb.Every.Duration).UTC()
	st.NextRunAt = &next

	quiet, err := config.ParseQuietHours(hb.QuietHours)
	if err != nil {
		return fmt.Errorf("heartbeat: quiet_hours: %w", err)
	}
	if quiet.Contains(now.Local()) {
		return e.saveHeartbeat(ctx, st)
	}

	var prev heartbeatState
	if st.Cursor != "" {
		// A cursor that does not parse is dropped: the worst case is one repeated wake.
		_ = json.Unmarshal([]byte(st.Cursor), &prev)
	}
	since := prev.BuiltAt
	if since.IsZero() {
		since = now.Add(-hbFirstWindow)
	}
	items, err := e.heartbeatItems(ctx, p, hb.StaleQueued.Duration, since)
	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}

	digest := HeartbeatDigest{Project: p.Name, At: now.UTC()}
	cur := heartbeatState{Seen: map[string]time.Time{}, BuiltAt: now.UTC()}
	for _, it := range items {
		first, known := prev.Seen[it.Key]
		if !known {
			first = now.UTC()
			digest.NewKeys = append(digest.NewKeys, it.Key)
		}
		it.FirstSeen = first
		cur.Seen[it.Key] = first
		digest.Items = append(digest.Items, it)
	}
	st.ItemCount = len(items)

	if len(digest.NewKeys) > 0 {
		// Not saved on failure, so the next tick tries again with the same new items.
		if err := e.d.Heartbeat.WakeHeartbeat(ctx, digest); err != nil {
			return fmt.Errorf("heartbeat: wake: %w", err)
		}
	}
	raw, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	st.Cursor = string(raw)
	return e.saveHeartbeat(ctx, st)
}

// saveHeartbeat stores the loop state it filled in. runLoopOnce reloads it afterwards and sets
// the run time and error, keeping these fields.
func (e *Engine) saveHeartbeat(ctx context.Context, st domain.LoopState) error {
	if err := e.d.Store.SaveLoopState(ctx, st); err != nil {
		return fmt.Errorf("heartbeat: save state: %w", err)
	}
	return nil
}

// heartbeatItems collects every attention item of project p, sorted by kind then entity. since
// starts the window for things that happen once (runs, deploys, results, self-update failures).
func (e *Engine) heartbeatItems(ctx context.Context, p *config.Project, stale time.Duration, since time.Time) ([]HeartbeatItem, error) {
	env := evidenceEnv(p)
	now := e.d.Now()
	var items []HeartbeatItem
	add := func(kind, id, reason, summary string) {
		items = append(items, HeartbeatItem{
			Key: kind + ":" + id + ":" + reasonKey(reason), Kind: kind, EntityID: id, Project: p.Name,
			Summary: summary,
		})
	}
	quote := func(s string) string { return firstRunes(oneLine(s, env), hbSummaryLimit) }

	tasks, err := e.d.Store.ListTasks(ctx, store.TaskFilter{Project: p.Name, Limit: hbScanLimit})
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	byID := make(map[string]domain.Task, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}
	for _, t := range tasks {
		switch t.Status {
		case domain.TaskBlocked, domain.TaskFailed:
			kind := hbBlockedTask
			if t.Status == domain.TaskFailed {
				kind = hbFailedTask
			}
			add(kind, t.ID, t.BlockedReason, fmt.Sprintf("task %q is %s after %d of %d attempts: %s",
				t.Title, t.Status, t.Attempts, t.MaxAttempts, quote(t.BlockedReason)))
		case domain.TaskQueued:
			if len(t.WaitingOn) == 0 && stale > 0 && now.Sub(t.UpdatedAt) >= stale {
				add(hbStaleQueued, t.ID, "", fmt.Sprintf("task %q has been queued for %s", t.Title, now.Sub(t.UpdatedAt).Round(time.Minute)))
			}
			for _, depID := range t.WaitingOn {
				dep, ok := byID[depID]
				if !ok {
					if dep, err = e.d.Store.GetTask(ctx, depID); err != nil {
						if errors.Is(err, store.ErrNotFound) {
							continue
						}
						return nil, fmt.Errorf("get task %s: %w", depID, err)
					}
				}
				if why := hbDependencyProblem(dep.Status); why != "" {
					add(hbDependency, t.ID, depID+" "+string(dep.Status), fmt.Sprintf("task %q waits on %s (%q), which is %s: %s",
						t.Title, depID, dep.Title, dep.Status, why))
				}
			}
		}
		if hbAsksForHuman(t) && !t.UpdatedAt.Before(since) {
			add(hbAgentQuestion, t.ID, t.Summary, fmt.Sprintf("task %q result needs the owner: %s", t.Title, quote(t.Summary)))
		}
	}

	goals, err := e.d.Store.ListGoals(ctx, store.GoalFilter{Project: p.Name, Statuses: []domain.GoalStatus{domain.GoalBlocked}})
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	for _, g := range goals {
		add(hbBlockedGoal, g.ID, g.Summary, fmt.Sprintf("goal %q is blocked: %s", g.Title, quote(g.Summary)))
	}

	runs, err := e.d.Store.ListRuns(ctx, store.RunFilter{
		Project: p.Name, Limit: hbScanLimit,
		Statuses: []domain.RunStatus{domain.RunStalled, domain.RunDenied, domain.RunFailed, domain.RunTimedOut},
	})
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	for _, r := range runs {
		ended := r.StartedAt
		if r.EndedAt != nil {
			ended = *r.EndedAt
		}
		if ended.Before(since) {
			continue
		}
		title := r.TaskID
		if t, ok := byID[r.TaskID]; ok {
			title = fmt.Sprintf("%s (%s)", t.Title, r.TaskID)
		}
		add(hbRun, r.ID, string(r.Status), fmt.Sprintf("%s run of %s %s: %s", r.Role, title, r.Status, quote(r.Error)))
	}

	approvals, err := e.d.Store.ListApprovals(ctx, domain.ApprovalPending)
	if err != nil {
		return nil, fmt.Errorf("list approvals: %w", err)
	}
	for _, a := range approvals {
		if a.Project == p.Name {
			add(hbApproval, a.ID, "", fmt.Sprintf("%s approval pending for task %s: %s", a.Kind, a.TaskID, quote(a.Title)))
		}
	}

	deps, err := e.d.Store.ListDeployments(ctx, p.Name, 50)
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	for _, d := range deps {
		if (d.State == domain.DeployFailed || d.State == domain.DeployRolledBack) && !d.UpdatedAt.Before(since) {
			add(hbDeploy, d.ID, string(d.State), fmt.Sprintf("deploy of %s %s: %s", shortSHA(d.Ref), d.State, quote(d.Notes)))
		}
	}

	states, err := e.d.Store.ListLoopStates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list loop states: %w", err)
	}
	for _, s := range states {
		if s.Project == p.Name && s.LastError != "" && s.Name != loopHeartbeat {
			add(hbLoopError, s.Name, s.LastError, fmt.Sprintf("loop %s is failing: %s", s.Name, quote(s.LastError)))
		}
	}

	// The self-update loop reports a failed update as a system event, not as a loop error.
	evs, err := e.d.Store.ListEvents(ctx, store.EventFilter{Project: p.Name, Limit: hbScanLimit})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	for _, ev := range evs {
		if ev.Type == domain.EvSystem && strings.HasPrefix(ev.Message, "self-update to ") && strings.HasSuffix(ev.Message, " failed") && !ev.Time.Before(since) {
			add(hbSelfUpdateFailure, fmt.Sprint(ev.ID), "", ev.Message)
		}
	}

	for _, pf := range e.Preflight() {
		if pf.Project == p.Name && !pf.OK {
			add(hbPreflight, p.Name, pf.Path+" "+pf.Error, "preflight failing: "+quote(pf.Summary()))
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].EntityID < items[j].EntityID
	})
	return items, nil
}

// hbDependencyProblem says what is wrong with a task another task waits on, or "" when nothing is.
func hbDependencyProblem(s domain.TaskStatus) string {
	switch s {
	case domain.TaskBlocked, domain.TaskFailed, domain.TaskCancelled:
		return "it will not land without help"
	case domain.TaskMerged, domain.TaskDeploying, domain.TaskDone:
		return "it already landed, so the wait should have cleared"
	}
	return ""
}

// hbAsksForHuman reports whether a task's latest result needs the owner. Blocked, failed, queued,
// running and cancelled tasks have their own items.
func hbAsksForHuman(t domain.Task) bool {
	switch t.Status {
	case domain.TaskBlocked, domain.TaskFailed, domain.TaskQueued, domain.TaskRunning, domain.TaskCancelled:
		return false
	}
	return t.Kind != domain.KindQuestion && resultNeedsHuman(t.Summary)
}

// humanPhrases are the words an agent uses when it cannot go on without a person.
var humanPhrases = []string{
	"needs a human", "need a human", "needs human", "need human",
	"needs the owner", "need the owner", "needs an owner", "need an owner",
	"waiting for the owner", "waiting on the owner", "owner decision", "owner to decide",
	"human intervention", "human review needed",
}

// resultNeedsHuman reports whether an agent's result text says it needs a human or ends with a
// question to the owner. It is conservative: a question that does not address the owner or the
// reader ("why does this fail?") does not count.
func resultNeedsHuman(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	for _, ph := range humanPhrases {
		if strings.Contains(text, ph) {
			return true
		}
	}
	lines := strings.Split(text, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasSuffix(last, "?") {
		return false
	}
	// Only the last sentence: an earlier "you" does not make the final question the owner's.
	if i := strings.LastIndexAny(strings.TrimSuffix(last, "?"), ".!"); i >= 0 {
		last = last[i+1:]
	}
	for _, w := range strings.FieldsFunc(last, func(r rune) bool { return !(r >= 'a' && r <= 'z') }) {
		switch w {
		case "owner", "you", "your", "yours":
			return true
		}
	}
	return false
}

// reasonKey fingerprints a reason so a changed reason is a new item. An empty reason has none.
func reasonKey(reason string) string {
	reason = strings.Join(strings.Fields(reason), " ")
	if reason == "" {
		return "-"
	}
	sum := sha256.Sum256([]byte(reason))
	return hex.EncodeToString(sum[:4])
}
