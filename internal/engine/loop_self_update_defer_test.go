package engine

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
)

// exitErr stands in for *exec.ExitError, which a test cannot build.
type exitErr int

func (e exitErr) Error() string { return "exit status " + strconv.Itoa(int(e)) }
func (e exitErr) ExitCode() int { return int(e) }

func (s *suEnv) loop() {
	s.t.Helper()
	if err := s.e.selfUpdateLoop(s.ctx, s.e.Config().Projects["demo"]); err != nil {
		s.t.Fatal(err)
	}
}

func (s *suEnv) liveRun(taskID string) {
	s.t.Helper()
	if _, err := s.st.CreateRun(s.ctx, domain.Run{
		TaskID: taskID, Project: "demo", Role: "implementer", Status: domain.RunRunning, StartedAt: s.now,
	}); err != nil {
		s.t.Fatal(err)
	}
}

func TestSelfUpdateDefersWhileBusy(t *testing.T) {
	t.Run("task run live", func(t *testing.T) {
		s := suNew(t)
		s.liveRun("task-1")
		s.loop()
		if n := s.started(); n != 0 {
			t.Fatalf("started %d updaters while a task run was live", n)
		}
	})
	t.Run("task queued", func(t *testing.T) {
		s := suNew(t)
		if _, err := s.st.CreateTask(context.Background(), domain.Task{
			Project: "demo", Kind: domain.KindFeature, Title: "wait", Status: domain.TaskQueued, MaxAttempts: 2,
		}); err != nil {
			t.Fatal(err)
		}
		s.loop()
		if n := s.started(); n != 0 {
			t.Fatalf("started %d updaters with a task queued", n)
		}
	})
}

func TestSelfUpdateIgnoresChatTurns(t *testing.T) {
	s := suNew(t)
	s.liveRun("") // an orchestrator chat or goal turn has no task
	s.loop()
	if n := s.started(); n != 1 {
		t.Fatalf("started %d updaters, want 1: chat turns are short and do not count", n)
	}
}

func TestSelfUpdateStartsWhenIdle(t *testing.T) {
	s := suNew(t)
	s.loop()
	if n := s.started(); n != 1 {
		t.Fatalf("started %d updaters on an idle factory, want 1", n)
	}
}

func TestSelfUpdateMaxDeferStartsAnyway(t *testing.T) {
	s := suNew(t)
	s.liveRun("task-1")
	// Eligible 10 minutes ago; max_defer is 2h by default.
	s.now = s.now.Add(time.Hour)
	s.loop()
	if n := s.started(); n != 0 {
		t.Fatalf("started %d updaters before max_defer", n)
	}
	s.now = s.now.Add(2 * time.Hour)
	s.loop()
	if n := s.started(); n != 1 {
		t.Fatalf("started %d updaters after max_defer, want 1", n)
	}
}

func TestSelfUpdateWaitIsRunTimeoutPlusFive(t *testing.T) {
	s := suNew(t)
	s.loop()
	want := "ONETT_UPDATE_WAIT=" + strconv.Itoa(int((s.e.Config().Limits.RunTimeout.Duration + 5*time.Minute).Seconds()))
	if env := strings.Join(s.cmds[0].Env, "\n"); !strings.Contains(env, want) {
		t.Fatalf("env lacks %s", want)
	}
}

func TestSelfUpdateBusyGiveUpBacksOffAnHour(t *testing.T) {
	s := suNew(t)
	ref := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	s.loop()
	s.waitC <- exitErr(selfUpdateBusyExit)
	deadline := time.Now().Add(5 * time.Second)
	for !s.e.selfUpd.backedOff(ref, s.now) {
		if time.Now().After(deadline) {
			t.Fatal("no backoff after the updater gave up")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := s.e.selfUpd.failures(ref); got != 0 {
		t.Fatalf("a busy give-up counted as %d failures", got)
	}
	// The updater has exited; the fake's lock names this live test process, so clear it and let
	// only the backoff hold the loop back.
	_ = os.Remove(filepath.Join(s.dir, selfUpdateLockFile))
	s.now = s.now.Add(59 * time.Minute)
	s.loop()
	if n := s.started(); n != 1 {
		t.Fatalf("restarted %d times inside the backoff", n-1)
	}
	s.now = s.now.Add(2 * time.Minute)
	_ = os.Remove(filepath.Join(s.dir, selfUpdateLockFile))
	s.loop()
	if n := s.started(); n != 2 {
		t.Fatalf("started %d updaters after the backoff, want 2", n)
	}
}
