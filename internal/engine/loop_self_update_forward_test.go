package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
)

// sufNew is a self-update test on a real repository: project demo is the self project and its
// clone is a local git repository, so the loop's ancestry checks run real git. No release is
// seeded; tests add their own.
func sufNew(t *testing.T) (*suEnv, lpRepo) {
	t.Helper()
	r := lpNewRepo(t)
	doc := lpRepoDoc(t, r, "    loops:\n      self_update:\n        enabled: true\n        command: ./update.sh\n", "self_project: demo\n")
	s := &suEnv{lpEnv: lpNewEngine(t, doc), waitC: make(chan error, 1)}
	s.dir = s.e.Config().Server.DataDir
	s.e.d.StartUpdater = func(cmd *exec.Cmd) (int, func() error, error) {
		s.mu.Lock()
		s.cmds = append(s.cmds, cmd)
		s.mu.Unlock()
		return os.Getpid(), func() error { return <-s.waitC }, nil
	}
	return s, r
}

// sufCommit adds a commit to origin's main, fetches it into the clone the engine reads, and
// returns its full ID.
func sufCommit(t *testing.T, r lpRepo, file string) string {
	t.Helper()
	sha := lpShippedCommit(t, r, file)
	lpGit(t, r.clone, "fetch", "origin")
	return sha
}

// sufLog returns self-update.log, or "" when it does not exist.
func sufLog(s *suEnv) string {
	b, err := os.ReadFile(filepath.Join(s.dir, selfUpdateLogFile))
	if err != nil {
		return ""
	}
	return string(b)
}

// The daemon was updated by hand to a commit that has no release of its own; the newest healthy
// release is older than what is running, so it is not an update.
func TestSelfUpdateNeverMovesBackToAnOlderRelease(t *testing.T) {
	s, r := sufNew(t)
	older := sufCommit(t, r, "older.txt")
	running := sufCommit(t, r, "running.txt")
	s.deploy(older, domain.DeployHealthy, 20*time.Minute)
	s.e.d.Version = running[:7]
	s.loop()
	s.loop()
	if n := s.started(); n != 0 {
		t.Fatalf("started %d updaters for %s; daemon at %s was moved back to an older release", n, s.startedRef(0), running)
	}
	if s.e.selfUpdateWaiting(s.ctx) {
		t.Fatalf("an older release must not hold the deploy loop back")
	}
}

func TestSelfUpdateMovesForwardToADescendantRelease(t *testing.T) {
	s, r := sufNew(t)
	running := sufCommit(t, r, "running.txt")
	newer := sufCommit(t, r, "newer.txt")
	s.deploy(newer, domain.DeployHealthy, 20*time.Minute)
	s.e.d.Version = running[:7]
	if !s.e.selfUpdateWaiting(s.ctx) {
		t.Fatalf("a healthy descendant release should hold the deploy loop back")
	}
	s.loop()
	if s.started() != 1 {
		t.Fatalf("started %d updaters, want 1", s.started())
	}
	if got := s.startedRef(0); got != newer {
		t.Fatalf("updater started for %s, want %s", got, newer)
	}
}

func TestSelfUpdateHoldsOnUnknownDirtyOrDevBuildAndSaysWhyOnce(t *testing.T) {
	cases := []struct {
		name    string
		version func(running string) string
	}{
		{"commit not in the clone", func(string) string { return "1111111" }},
		{"dirty build", func(running string) string { return running[:7] + "-dirty" }},
		{"dev build", func(string) string { return "dev" }},
		{"dev build with a commit", func(running string) string { return "dev+" + running[:12] }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r := sufNew(t)
			running := sufCommit(t, r, "running.txt")
			newer := sufCommit(t, r, "newer.txt")
			s.deploy(newer, domain.DeployHealthy, 20*time.Minute)
			version := tc.version(running)
			s.e.d.Version = version
			for range 3 {
				s.run(loopSelfUpdate)
			}
			if n := s.started(); n != 0 {
				t.Fatalf("started %d updaters for %s while running %s, want none", n, s.startedRef(0), version)
			}
			if s.e.selfUpdateWaiting(s.ctx) {
				t.Fatalf("an unplaceable build must not hold the deploy loop back")
			}
			// The reason is logged once, not on every pass.
			if n := strings.Count(sufLog(s), " onett: not updating: "); n != 1 {
				t.Fatalf("reason logged %d times, want once; log:\n%s", n, sufLog(s))
			}
			if !strings.Contains(sufLog(s), version) {
				t.Fatalf("log does not name the running version %q; log:\n%s", version, sufLog(s))
			}
			st, err := s.st.GetLoopState(s.ctx, loopSelfUpdate, "demo")
			if err != nil {
				t.Fatalf("get loop state: %v", err)
			}
			if !strings.Contains(st.Cursor, version) {
				t.Fatalf("loop state cursor %q does not keep the reason for %q", st.Cursor, version)
			}

			// Once the build is one the loop can place, the update goes ahead and the reason is cleared.
			s.e.d.Version = running[:7]
			s.run(loopSelfUpdate)
			if s.started() != 1 {
				t.Fatalf("started %d updaters after the build became placeable, want 1", s.started())
			}
			if got := s.startedRef(0); got != newer {
				t.Fatalf("updater started for %s, want %s", got, newer)
			}
			st, err = s.st.GetLoopState(s.ctx, loopSelfUpdate, "demo")
			if err != nil {
				t.Fatalf("get loop state: %v", err)
			}
			if st.Cursor != "" {
				t.Fatalf("loop state cursor %q should be cleared once the update starts", st.Cursor)
			}
		})
	}
}

// After a rollback the daemon runs the old build again. The rolled-back release must not be
// retried, and the loop must not fall back to a release that is older than the running build.
func TestSelfUpdateRolledBackUpdateIsNotRetriedOrReplacedByOlderRelease(t *testing.T) {
	s, r := sufNew(t)
	older := sufCommit(t, r, "older.txt")
	running := sufCommit(t, r, "running.txt")
	target := sufCommit(t, r, "target.txt")
	s.deploy(older, domain.DeployHealthy, 30*time.Minute)
	s.deploy(target, domain.DeployHealthy, 20*time.Minute)
	s.e.d.Version = running[:7]
	result := "old=" + running[:7] + "\nnew=" + target[:7] + "\nref=" + target + "\nstatus=rolled_back\n"
	if err := os.WriteFile(filepath.Join(s.dir, selfUpdateResultFile), []byte(result), 0o600); err != nil {
		t.Fatalf("write result file: %v", err)
	}
	s.e.reportSelfUpdate(s.ctx)
	s.loop()
	s.loop()
	s.loop()
	if n := s.started(); n != 0 {
		t.Fatalf("started %d updaters for %s; want none (older=%s, target=%s): the rolled-back release was retried or an older release was used", n, s.startedRef(0), older, target)
	}
	if s.e.selfUpdateWaiting(s.ctx) {
		t.Fatalf("a rolled-back release must not hold the deploy loop back")
	}
}

// The owner updated by hand (update.sh from their own shell) to a commit newer than every staging
// release. The new daemon reports the update, and the loop leaves it there without calling that a
// reason it cannot tell.
func TestSelfUpdateLeavesAHandUpdateNewerThanEveryRelease(t *testing.T) {
	s, r := sufNew(t)
	r1 := sufCommit(t, r, "r1.txt")
	r2 := sufCommit(t, r, "r2.txt")
	hand := sufCommit(t, r, "hand.txt")
	s.deploy(r1, domain.DeployHealthy, 6*time.Hour)
	s.deploy(r2, domain.DeployHealthy, 5*time.Hour)
	s.e.d.Version = hand[:7]
	result := "old=" + r2[:7] + "\nnew=" + hand[:7] + "\nref=" + hand + "\nstatus=ok\n"
	if err := os.WriteFile(filepath.Join(s.dir, selfUpdateResultFile), []byte(result), 0o600); err != nil {
		t.Fatalf("write result file: %v", err)
	}
	s.e.reportSelfUpdate(s.ctx)
	for range 3 {
		s.run(loopSelfUpdate)
	}
	if n := s.started(); n != 0 {
		t.Fatalf("started %d updaters for %s; daemon at %s is newer than r1 %s and r2 %s", n, s.startedRef(0), hand, r1, r2)
	}
	if s.e.selfUpdateWaiting(s.ctx) {
		t.Fatal("no release is newer than the running build; the deploy loop must not hold back")
	}
	if !strings.Contains(sufLog(s), "update ok: now running "+hand[:7]) {
		t.Fatalf("log does not record the update; log:\n%s", sufLog(s))
	}
	if strings.Contains(sufLog(s), " onett: not updating: ") {
		t.Fatalf("a build newer than every release is not a reason to hold; log:\n%s", sufLog(s))
	}
	st, err := s.st.GetLoopState(s.ctx, loopSelfUpdate, "demo")
	if err != nil {
		t.Fatalf("get loop state: %v", err)
	}
	if st.Cursor != "" {
		t.Fatalf("loop state cursor %q should be empty", st.Cursor)
	}
}

// Nothing fetches the clone when a release merges, so the loop fetches once before it gives up on
// comparing a release it has not seen.
func TestSelfUpdateFetchesAReleaseTheCloneLacks(t *testing.T) {
	s, r := sufNew(t)
	running := sufCommit(t, r, "running.txt")
	newer := lpShippedCommit(t, r, "newer.txt") // on origin, not fetched into the clone
	s.deploy(newer, domain.DeployHealthy, 20*time.Minute)
	s.e.d.Version = running[:7]
	s.loop()
	if s.started() != 1 {
		t.Fatalf("started %d updaters, want 1", s.started())
	}
	if got := s.startedRef(0); got != newer {
		t.Fatalf("updater started for %s, want %s", got, newer)
	}
}

// A fetch that fails is tried again on the next pass: the release's commit is still missing, and
// giving up after one attempt would hold a good update back.
func TestSelfUpdateRetriesAFetchThatFailed(t *testing.T) {
	s, r := sufNew(t)
	running := sufCommit(t, r, "running.txt")
	newer := lpShippedCommit(t, r, "newer.txt") // on origin, not fetched into the clone
	s.deploy(newer, domain.DeployHealthy, 20*time.Minute)
	s.e.d.Version = running[:7]

	gone := r.remote + ".gone" // the remote cannot be reached
	if err := os.Rename(r.remote, gone); err != nil {
		t.Fatal(err)
	}
	s.loop()
	if n := s.started(); n != 0 {
		t.Fatalf("started %d updaters for a release the clone does not have", n)
	}

	if err := os.Rename(gone, r.remote); err != nil { // the remote is back
		t.Fatal(err)
	}
	s.loop()
	if s.started() != 1 {
		t.Fatalf("started %d updaters after the remote came back, want 1", s.started())
	}
	if got := s.startedRef(0); got != newer {
		t.Fatalf("updater started for %s, want %s", got, newer)
	}
}
