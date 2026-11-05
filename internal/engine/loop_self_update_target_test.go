package engine

import (
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
)

const (
	suRefA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	suRefB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	suRefC = "cccccccccccccccccccccccccccccccccccccccc"
)

// suStarvedEnv has an older running daemon and no seeded release: tests add their own.
func suStarvedEnv(t *testing.T) *suEnv {
	t.Helper()
	s := suNew(t)
	deps, err := s.st.ListDeployments(s.ctx, "demo", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range deps { // drop the default release b
		if _, err := s.st.UpdateDeployment(s.ctx, d.ID, func(x *domain.Deployment) error {
			x.State = domain.DeployFailed
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func (s *suEnv) startedRef(i int) string {
	s.t.Helper()
	for _, kv := range s.cmds[i].Env {
		if len(kv) > 16 && kv[:16] == "ONETT_UPDATE_REF" {
			return kv[17:]
		}
	}
	return ""
}

// Releases keep landing: A is old enough, B is not, C is in flight. The update waits for C, the
// deploy loop starts nothing meanwhile, and then the newest release that qualifies is the target.
func TestSelfUpdateTargetsNewestQualifyingWhileReleasesLand(t *testing.T) {
	s := suStarvedEnv(t)
	s.deploy(suRefA, domain.DeployHealthy, 15*time.Minute)
	s.deploy(suRefB, domain.DeployHealthy, 4*time.Minute)
	s.deploy(suRefC, domain.DeployDeploying, 0)

	s.loop()
	if n := s.started(); n != 0 {
		t.Fatalf("started %d updaters with a release in flight", n)
	}
	if !s.e.selfUpdateWaiting(s.ctx) {
		t.Fatal("an eligible target should make the deploy loop hold back")
	}
	deps, _ := s.st.ListDeployments(s.ctx, "demo", 10)
	for _, d := range deps {
		if d.Ref == suRefC {
			if _, err := s.st.UpdateDeployment(s.ctx, d.ID, func(x *domain.Deployment) error {
				x.State = domain.DeployHealthy
				x.UpdatedAt = s.now.Add(-time.Minute)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	s.loop()
	if n := s.started(); n != 1 {
		t.Fatalf("started %d updaters, want 1", n)
	}
	if got := s.startedRef(0); got != suRefA {
		t.Fatalf("target = %s, want A (newest healthy for min_healthy)", got)
	}
}

// While an eligible self-update waits, the deploy loop leaves merged work alone; without one it
// starts the release as usual.
func TestDeployLoopHoldsNewReleaseForEligibleSelfUpdate(t *testing.T) {
	doc := lpDoc(t, lpPath(t),
		"    deploy:\n      type: test\n      verify_for: 10m\n    loops:\n      self_update:\n        enabled: true\n        command: ./update.sh\n",
		"self_project: demo\n")
	env := lpNewEngine(t, doc)
	env.e.d.Version = "v0.1-3-g" + suRef[:7]
	lpMergedTask(t, env, "Add notes", "sha-1")
	at := env.now.Add(-20 * time.Minute)
	if _, err := env.st.CreateDeployment(env.ctx, domain.Deployment{
		Project: "demo", Ref: suRefA, State: domain.DeployHealthy, StartedAt: at, UpdatedAt: at,
	}); err != nil {
		t.Fatal(err)
	}

	env.run(loopDeployWatch)
	if len(env.dep.deployed) != 0 {
		t.Fatalf("deploy loop started %v while a self-update waits", env.dep.deployed)
	}

	env.e.d.Version = "v0.1-3-g" + suRefA[:7] // the daemon now runs A: nothing to wait for
	env.run(loopDeployWatch)
	if len(env.dep.deployed) != 1 {
		t.Fatalf("deploy loop did not start the release: %v", env.dep.deployed)
	}
}

func TestSelfUpdateSkipsWhenRunningRefIsNewestQualifying(t *testing.T) {
	s := suStarvedEnv(t)
	s.deploy(suRefA, domain.DeployHealthy, 30*time.Minute)
	s.deploy(suRefB, domain.DeployHealthy, 20*time.Minute)
	s.deploy(suRefC, domain.DeployHealthy, 2*time.Minute) // too recent
	s.e.d.Version = "v0.1-3-g" + suRefB[:7]
	s.loop()
	if n := s.started(); n != 0 {
		t.Fatalf("started %d updaters, want none", n)
	}
	if s.e.selfUpdateWaiting(s.ctx) {
		t.Fatal("no target is eligible")
	}
}

func TestSelfUpdateFallsBackFromRefAtFailureLimit(t *testing.T) {
	s := suStarvedEnv(t)
	s.deploy(suRefA, domain.DeployHealthy, 30*time.Minute)
	s.deploy(suRefB, domain.DeployHealthy, 20*time.Minute)
	s.e.selfUpd.fail(suRefB, selfUpdateMaxFailures)
	s.loop()
	if s.started() != 1 || s.startedRef(0) != suRefA {
		t.Fatalf("want one updater for A, got %d (%s)", s.started(), s.startedRef(0))
	}
}

func TestSelfUpdateNothingNewerThanRunningAfterFailureLimit(t *testing.T) {
	s := suStarvedEnv(t)
	s.deploy(suRefA, domain.DeployHealthy, 30*time.Minute) // older than the running daemon
	s.deploy(suRefB, domain.DeployHealthy, 20*time.Minute)
	s.e.d.Version = "v0.1-3-g" + suRefA[:7]
	s.e.selfUpd.fail(suRefB, selfUpdateMaxFailures)
	s.loop()
	if n := s.started(); n != 0 {
		t.Fatalf("started %d updaters, want none", n)
	}
}

// Every candidate is too young, or the newer release failed: nothing starts, and an older
// eligible release is still chosen over a failed newer one.
func TestSelfUpdateTargetHoldOffAndFailedNewer(t *testing.T) {
	s := suStarvedEnv(t)
	s.deploy(suRefA, domain.DeployHealthy, 3*time.Minute)
	s.deploy(suRefB, domain.DeployHealthy, 1*time.Minute)
	s.loop()
	if n := s.started(); n != 0 {
		t.Fatalf("started %d updaters with only young releases, want none", n)
	}
	if s.e.selfUpdateWaiting(s.ctx) {
		t.Fatal("young releases are not an eligible target")
	}

	s = suStarvedEnv(t)
	s.deploy(suRefA, domain.DeployHealthy, 30*time.Minute)
	s.deploy(suRefB, domain.DeployRolledBack, 5*time.Minute)
	s.loop()
	if s.started() != 1 || s.startedRef(0) != suRefA {
		t.Fatalf("want one updater for A, got %d (%s)", s.started(), s.startedRef(0))
	}
}

// A paused self project never updates, so it must not make the deploy loop hold releases back.
func TestSelfUpdatePausedProjectDoesNotHoldReleases(t *testing.T) {
	s := suStarvedEnv(t)
	s.deploy(suRefA, domain.DeployHealthy, 30*time.Minute)
	if !s.e.selfUpdateWaiting(s.ctx) {
		t.Fatal("precondition: A should be eligible")
	}
	s.e.paused.Store(true)
	if s.e.selfUpdateWaiting(s.ctx) {
		t.Fatal("a paused project must not hold releases back")
	}
}
