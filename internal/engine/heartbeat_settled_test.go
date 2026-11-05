package engine

import (
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestHeartbeatSkipsRunsOfSettledTasks(t *testing.T) {
	env, rec := hbNewEnv(t, "")
	at := env.now.Add(-10 * time.Minute)
	mk := func(status domain.TaskStatus, rs domain.RunStatus) domain.Run {
		task := lpRetroTask(t, env, domain.Task{Title: string(status), Status: status})
		r, err := env.st.CreateRun(env.ctx, domain.Run{TaskID: task.ID, Project: "demo", Role: "implementer", Status: rs, StartedAt: at, EndedAt: &at})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	doneStalled := mk(domain.TaskDone, domain.RunStalled)
	cancelledDenied := mk(domain.TaskCancelled, domain.RunDenied)
	mergedFailed := mk(domain.TaskMerged, domain.RunFailed)
	deployingFailed := mk(domain.TaskDeploying, domain.RunTimedOut)
	blockedFailed := mk(domain.TaskBlocked, domain.RunFailed)

	env.run(loopHeartbeat)
	if len(rec.digests) != 1 {
		t.Fatalf("wakes = %d, want 1", len(rec.digests))
	}
	d := rec.digests[0]
	for _, r := range []domain.Run{doneStalled, cancelledDenied, mergedFailed, deployingFailed} {
		if hbHas(d, hbRun, r.ID) {
			t.Errorf("run %s of a settled task is in the digest", r.ID)
		}
	}
	if !hbHas(d, hbRun, blockedFailed.ID) {
		t.Errorf("run of a blocked task missing; has %v", hbKinds(d))
	}
}

func TestHeartbeatFirstWindowFollowsInterval(t *testing.T) {
	if got := hbFirstWindow(15 * time.Minute); got != time.Hour {
		t.Errorf("window for 15m = %s, want 1h", got)
	}
	if got := hbFirstWindow(3 * time.Hour); got != 3*time.Hour {
		t.Errorf("window for 3h = %s, want 3h", got)
	}

	env, rec := hbNewEnv(t, "")
	task := lpRetroTask(t, env, domain.Task{Title: "T", Status: domain.TaskBlocked, BlockedReason: "x"})
	old := env.now.Add(-3 * time.Hour)
	r, err := env.st.CreateRun(env.ctx, domain.Run{TaskID: task.ID, Project: "demo", Role: "implementer", Status: domain.RunStalled, StartedAt: old, EndedAt: &old})
	if err != nil {
		t.Fatal(err)
	}
	env.run(loopHeartbeat)
	if len(rec.digests) != 1 || hbHas(rec.digests[0], hbRun, r.ID) {
		t.Errorf("first tick replayed a run older than the window: %+v", rec.digests)
	}
}
