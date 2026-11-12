package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
)

// hbRecorder is a HeartbeatWaker that keeps the digests it was woken with.
type hbRecorder struct {
	digests []HeartbeatDigest
	err     error
}

func (r *hbRecorder) WakeHeartbeat(_ context.Context, d HeartbeatDigest) error {
	if r.err != nil {
		return r.err
	}
	r.digests = append(r.digests, d)
	return nil
}

func hbNewEnv(t *testing.T, topExtra string) (*lpEnv, *hbRecorder) {
	t.Helper()
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", topExtra))
	rec := &hbRecorder{}
	env.e.d.Heartbeat = rec
	return env, rec
}

// hbKinds returns the digest's item kinds.
func hbKinds(d HeartbeatDigest) []string {
	var out []string
	for _, it := range d.Items {
		out = append(out, it.Kind)
	}
	return out
}

func hbHas(d HeartbeatDigest, kind, id string) bool {
	for _, it := range d.Items {
		if it.Kind == kind && it.EntityID == id {
			return true
		}
	}
	return false
}

func TestHeartbeatDigestItems(t *testing.T) {
	env, rec := hbNewEnv(t, "")
	old := env.now.Add(-5 * time.Hour)
	recent := env.now.Add(-time.Hour)

	blocked := lpRetroTask(t, env, domain.Task{Title: "Blocked one", Status: domain.TaskBlocked, BlockedReason: "needs a key", Attempts: 2, MaxAttempts: 3})
	failed := lpRetroTask(t, env, domain.Task{Title: "Failed one", Status: domain.TaskFailed, BlockedReason: "tests red"})
	staleQ := lpRetroTask(t, env, domain.Task{Title: "Stale", Status: domain.TaskQueued, UpdatedAt: old})
	freshQ := lpRetroTask(t, env, domain.Task{Title: "Fresh", Status: domain.TaskQueued, UpdatedAt: recent})
	depBlocked := lpRetroTask(t, env, domain.Task{Title: "Dep blocked", Status: domain.TaskBlocked, BlockedReason: "x"})
	depLanded := lpRetroTask(t, env, domain.Task{Title: "Dep landed", Status: domain.TaskMerged})
	waitBlocked := lpRetroTask(t, env, domain.Task{Title: "Waits on blocked", Status: domain.TaskQueued, UpdatedAt: old, WaitingOn: []string{depBlocked.ID}})
	waitLanded := lpRetroTask(t, env, domain.Task{Title: "Waits on landed", Status: domain.TaskQueued, UpdatedAt: recent, WaitingOn: []string{depLanded.ID}})
	asks := lpRetroTask(t, env, domain.Task{Title: "Asks", Status: domain.TaskReview, Summary: "Done, but this needs a human to pick the name."})

	g, err := env.st.CreateGoal(env.ctx, domain.Goal{Project: "demo", Title: "Goal", Status: domain.GoalBlocked, Summary: "no progress"})
	if err != nil {
		t.Fatal(err)
	}
	ended := env.now.Add(-30 * time.Minute)
	mkRun := func(status domain.RunStatus, at time.Time) domain.Run {
		r, err := env.st.CreateRun(env.ctx, domain.Run{TaskID: blocked.ID, Project: "demo", Role: "implementer", Status: status, StartedAt: at, EndedAt: &at})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	stalled, denied := mkRun(domain.RunStalled, ended), mkRun(domain.RunDenied, ended)
	oldFailed := mkRun(domain.RunFailed, env.now.Add(-48*time.Hour))
	mkRun(domain.RunSucceeded, ended)

	ap, err := env.st.CreateApproval(env.ctx, domain.Approval{TaskID: blocked.ID, Project: "demo", Kind: "merge", Title: "Merge it", Status: domain.ApprovalPending})
	if err != nil {
		t.Fatal(err)
	}
	dep, err := env.st.CreateDeployment(env.ctx, domain.Deployment{Project: "demo", Ref: "abcdef1234", State: domain.DeployRolledBack, StartedAt: ended, UpdatedAt: ended})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.st.SaveLoopState(env.ctx, domain.LoopState{Name: loopFriction, Project: "demo", LastError: "gh: boom"}); err != nil {
		t.Fatal(err)
	}
	env.e.recordSelfUpdateFailure(env.ctx, "demo", "abcdef1234", "self-update to abcdef1 failed")

	env.run(loopHeartbeat)
	if len(rec.digests) != 1 {
		t.Fatalf("wakes = %d, want 1", len(rec.digests))
	}
	d := rec.digests[0]
	for _, w := range []struct{ kind, id string }{
		{hbBlockedTask, blocked.ID}, {hbFailedTask, failed.ID}, {hbStaleQueued, staleQ.ID},
		{hbDependency, waitBlocked.ID}, {hbDependency, waitLanded.ID}, {hbAgentQuestion, asks.ID},
		{hbBlockedGoal, g.ID}, {hbRun, stalled.ID}, {hbRun, denied.ID}, {hbApproval, ap.ID},
		{hbDeploy, dep.ID}, {hbLoopError, loopFriction},
	} {
		if !hbHas(d, w.kind, w.id) {
			t.Errorf("digest lacks %s %s; has %v", w.kind, w.id, hbKinds(d))
		}
	}
	for _, id := range []string{freshQ.ID, waitBlocked.ID} {
		if hbHas(d, hbStaleQueued, id) {
			t.Errorf("task %s counted as stale", id)
		}
	}
	if hbHas(d, hbRun, oldFailed.ID) {
		t.Error("a run from before the window is in the digest")
	}
	var su bool
	for _, it := range d.Items {
		su = su || it.Kind == hbSelfUpdateFailure
		if it.Project != "demo" || it.Summary == "" || it.Key == "" || it.FirstSeen.IsZero() {
			t.Errorf("incomplete item %+v", it)
		}
	}
	if !su {
		t.Errorf("no self-update failure item; has %v", hbKinds(d))
	}
	if len(d.NewKeys) != len(d.Items) {
		t.Errorf("first digest: %d new of %d items", len(d.NewKeys), len(d.Items))
	}
	st, _ := env.st.GetLoopState(env.ctx, loopHeartbeat, "demo")
	if st.ItemCount != len(d.Items) || st.NextRunAt == nil || !st.NextRunAt.Equal(env.now.Add(15*time.Minute)) || st.LastRunAt.IsZero() {
		t.Errorf("loop state = %+v", st)
	}
}

func TestHeartbeatNoWakeOnUnchangedDigest(t *testing.T) {
	env, rec := hbNewEnv(t, "")
	lpRetroTask(t, env, domain.Task{Title: "Blocked", Status: domain.TaskBlocked, BlockedReason: "stuck"})
	env.run(loopHeartbeat)
	first := rec.digests[0].Items[0].FirstSeen
	env.now = env.now.Add(time.Hour)
	env.run(loopHeartbeat)
	if len(rec.digests) != 1 {
		t.Fatalf("wakes = %d after an unchanged digest, want 1", len(rec.digests))
	}

	// All memory is in the store, so a restarted daemon behaves the same: still no wake.
	rec2 := &hbRecorder{}
	env.e.d.Heartbeat = rec2
	env.run(loopHeartbeat)
	if len(rec2.digests) != 0 {
		t.Fatalf("woke again after restart")
	}

	lpRetroTask(t, env, domain.Task{Title: "Blocked too", Status: domain.TaskBlocked, BlockedReason: "also stuck"})
	env.now = env.now.Add(time.Hour)
	env.run(loopHeartbeat)
	if len(rec2.digests) != 1 || len(rec2.digests[0].NewKeys) != 1 || len(rec2.digests[0].Items) != 2 {
		t.Fatalf("digests = %+v, want one wake with 1 new of 2 items", rec2.digests)
	}
	kept := false
	for _, it := range rec2.digests[0].Items {
		kept = kept || it.FirstSeen.Equal(first)
	}
	if !kept {
		t.Errorf("first seen time of the old item was not kept")
	}
}

func TestHeartbeatChangedReasonIsNew(t *testing.T) {
	env, rec := hbNewEnv(t, "")
	task := lpRetroTask(t, env, domain.Task{Title: "T", Status: domain.TaskBlocked, BlockedReason: "one"})
	env.run(loopHeartbeat)
	if _, err := env.st.UpdateTask(env.ctx, task.ID, func(x *domain.Task) error { x.BlockedReason = "two"; return nil }); err != nil {
		t.Fatal(err)
	}
	env.run(loopHeartbeat)
	if len(rec.digests) != 2 {
		t.Fatalf("wakes = %d, want 2", len(rec.digests))
	}
}

func TestHeartbeatEmptyDigestDoesNotWake(t *testing.T) {
	env, rec := hbNewEnv(t, "")
	env.run(loopHeartbeat)
	if len(rec.digests) != 0 {
		t.Fatalf("woke on an empty digest")
	}
}

func TestHeartbeatFailedWakeRetries(t *testing.T) {
	env, rec := hbNewEnv(t, "")
	lpRetroTask(t, env, domain.Task{Title: "Blocked", Status: domain.TaskBlocked, BlockedReason: "stuck"})
	rec.err = errors.New("down")
	if err := env.e.RunLoop(env.ctx, loopHeartbeat, "demo"); err == nil {
		t.Fatal("want the wake error")
	}
	rec.err = nil
	env.run(loopHeartbeat)
	if len(rec.digests) != 1 {
		t.Fatalf("wakes = %d, want 1 after the retry", len(rec.digests))
	}
}

func TestHeartbeatQuietHours(t *testing.T) {
	const quiet = "loops:\n  heartbeat:\n    quiet_hours: \"22:00-07:00\"\n"
	at := func(h, m int) time.Time { return time.Date(2026, 10, 27, h, m, 0, 0, time.Local) }
	for _, tc := range []struct {
		name string
		now  time.Time
		wake bool
	}{
		{"late evening is inside the wrapped window", at(23, 30), false},
		{"after midnight is inside the wrapped window", at(3, 0), false},
		{"the start is inside", at(22, 0), false},
		{"the end is outside", at(7, 0), true},
		{"midday is outside", at(12, 0), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, rec := hbNewEnv(t, quiet)
			env.now = tc.now
			lpRetroTask(t, env, domain.Task{Title: "Blocked", Status: domain.TaskBlocked, BlockedReason: "stuck"})
			env.run(loopHeartbeat)
			if got := len(rec.digests) == 1; got != tc.wake {
				t.Fatalf("woke = %v, want %v", got, tc.wake)
			}
			if !tc.wake {
				// What happened in the quiet hours is reported when they end.
				env.now = at(8, 0)
				env.run(loopHeartbeat)
				if len(rec.digests) != 1 {
					t.Fatalf("wakes after quiet hours = %d, want 1", len(rec.digests))
				}
			}
		})
	}
}

func TestHeartbeatQuietHoursKeepsRunsFromTheWindow(t *testing.T) {
	const quiet = "loops:\n  heartbeat:\n    quiet_hours: \"22:00-07:00\"\n"
	env, rec := hbNewEnv(t, quiet)
	env.now = time.Date(2026, 10, 27, 20, 0, 0, 0, time.Local)
	env.run(loopHeartbeat) // builds a digest at 20:00; nothing to report
	task := lpRetroTask(t, env, domain.Task{Title: "T", Status: domain.TaskRunning})
	at := time.Date(2026, 10, 27, 23, 0, 0, 0, time.Local)
	if _, err := env.st.CreateRun(env.ctx, domain.Run{TaskID: task.ID, Project: "demo", Role: "implementer", Status: domain.RunStalled, StartedAt: at, EndedAt: &at}); err != nil {
		t.Fatal(err)
	}
	env.now = time.Date(2026, 10, 27, 23, 30, 0, 0, time.Local)
	env.run(loopHeartbeat) // quiet: skipped
	env.now = time.Date(2026, 10, 28, 8, 0, 0, 0, time.Local)
	env.run(loopHeartbeat)
	if len(rec.digests) != 1 || !strings.Contains(strings.Join(hbKinds(rec.digests[0]), ","), hbRun) {
		t.Fatalf("digests = %+v, want one with the stalled run", rec.digests)
	}
}

func TestResultNeedsHuman(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"", false},
		{"Added the flag and tests; all green.", false},
		{"This needs a human to decide.", true},
		{"Blocked: NEEDS THE OWNER to rotate the key.", true},
		{"I need the owner's approval for the schema change.", true},
		{"Done.\nShould I also rename the package, owner?", true},
		{"Implemented. Do you want me to split the PR?", true},
		{"Which database should we use?", false},
		{"Why does this fail? It is fixed now.", false},
		{"You asked about caching? Done: the cache is on.\nAll tests pass.", false},
		{"Please advise, what do you prefer. Is that ok?", false},
		{"Fixed the parser. Is that what you meant?", true},
	} {
		if got := resultNeedsHuman(tc.text); got != tc.want {
			t.Errorf("resultNeedsHuman(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}
