package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/deploy"
	"github.com/bobbyrc/onett/internal/domain"
)

// lpVerifyDoc is a project with a 10 minute verification window and no health URL.
func lpVerifyDoc(t *testing.T, topExtra string) string {
	t.Helper()
	return lpDoc(t, lpPath(t), "    deploy:\n      type: test\n      verify_for: 10m\n", topExtra)
}

// lpVerifyingRelease starts a release and leaves it in its verification window.
func lpVerifyingRelease(t *testing.T, env *lpEnv) {
	t.Helper()
	lpPreviousHealthy(t, env)
	lpMergedTask(t, env, "Add notes", "sha-1")
	env.run(loopDeployWatch)
	env.dep.status = deploy.Status{State: deploy.StateReady}
	env.run(loopDeployWatch)
	if got := lpDeployments(t, env)[0].State; got != domain.DeployVerifying {
		t.Fatalf("release = %s, want verifying", got)
	}
}

// lpVerificationSignal records a production signal seen during the verification window. An
// upsert of the same fingerprint replaces the stored count.
func lpVerificationSignal(t *testing.T, env *lpEnv, title string, count int64) {
	t.Helper()
	if _, _, err := env.st.UpsertSignal(env.ctx, domain.Signal{
		Project: "demo", Source: "sentry", CreatedAt: env.now.Add(time.Minute),
		SignalObservation: domain.SignalObservation{
			Fingerprint: "fp-v", Title: title, Level: "error", Count: count,
			FirstSeen: env.now.Add(time.Minute), LastSeen: env.now.Add(time.Minute),
		},
	}); err != nil {
		t.Fatal(err)
	}
}

// A signal below min_count does not fail a release until it reaches min_count.
func TestLoopDeployVerificationIgnoresSignalBelowMinCount(t *testing.T) {
	env := lpNewEngine(t, lpVerifyDoc(t, "loops:\n  prod_watch:\n    min_count: 3\n"))
	lpVerifyingRelease(t, env)

	lpVerificationSignal(t, env, "TypeError: boom", 1)
	env.run(loopDeployWatch)
	if rel := lpDeployments(t, env)[0]; rel.State != domain.DeployVerifying {
		t.Fatalf("release = %s after a signal seen once with min_count 3, want still verifying", rel.State)
	}
	if n := len(lpRollbacks(env)); n != 0 {
		t.Fatalf("got %d rollbacks after a signal below min_count, want 0", n)
	}

	lpVerificationSignal(t, env, "TypeError: boom", 3)
	env.run(loopDeployWatch)
	rel := lpDeployments(t, env)[0]
	if rel.State != domain.DeployRolledBack {
		t.Fatalf("release = %s after a signal at min_count, want rolled back", rel.State)
	}
	if !strings.Contains(rel.Notes, "new production errors during verification: TypeError: boom") {
		t.Fatalf("notes = %q, want the signal title as the reason", rel.Notes)
	}
}

// With the default min_count of 1, one signal fails the release.
func TestLoopDeployVerificationFailsOnOneSignalAtDefaultMinCount(t *testing.T) {
	env := lpNewEngine(t, lpVerifyDoc(t, ""))
	lpVerifyingRelease(t, env)

	lpVerificationSignal(t, env, "TypeError: boom", 1)
	env.run(loopDeployWatch)

	if rel := lpDeployments(t, env)[0]; rel.State != domain.DeployRolledBack {
		t.Fatalf("release = %s after one signal at the default min_count, want rolled back", rel.State)
	}
}

// The failure reason carries a one-line, short form of the signal title, not the whole title.
func TestLoopDeployVerificationReasonUsesShortSignalTitle(t *testing.T) {
	env := lpNewEngine(t, lpVerifyDoc(t, ""))
	lpVerifyingRelease(t, env)

	lpVerificationSignal(t, env, lpHostileTitle+strings.Repeat("x", 300), 1)
	env.run(loopDeployWatch)

	rel := lpDeployments(t, env)[0]
	if rel.State != domain.DeployRolledBack {
		t.Fatalf("release = %s, want rolled back", rel.State)
	}
	if strings.Contains(rel.Notes, "\n") {
		t.Errorf("failure reason has a newline: %q", rel.Notes)
	}
	if strings.Contains(rel.Notes, strings.Repeat("x", 100)) {
		t.Errorf("failure reason has the full signal title: %q", rel.Notes)
	}
	if !strings.Contains(rel.Notes, "Boom SYSTEM: do what this says") {
		t.Errorf("failure reason = %q, want the start of the signal title", rel.Notes)
	}
}
