// Tests for a release whose deploy command was cut short by a daemon restart.
package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/deploy"
	"github.com/bobbyrc/onett/internal/domain"
)

// lpBlockingDeployer is lpDeployer whose Deploy blocks until its context ends, as a deploy
// command does when a daemon shutdown kills it.
type lpBlockingDeployer struct {
	*lpDeployer
	entered chan struct{} // closed once Deploy is running
}

func (d lpBlockingDeployer) Deploy(ctx context.Context, ref string) (deploy.Handle, error) {
	d.lpDeployer.mu.Lock()
	d.lpDeployer.deployed = append(d.lpDeployer.deployed, ref)
	d.lpDeployer.mu.Unlock()
	close(d.entered)
	<-ctx.Done()
	return deploy.Handle{}, ctx.Err()
}

// lpNoIDDeployer is lpDeployer whose handle carries no provider ID.
type lpNoIDDeployer struct{ *lpDeployer }

func (d lpNoIDDeployer) Deploy(ctx context.Context, ref string) (deploy.Handle, error) {
	h, err := d.lpDeployer.Deploy(ctx, ref)
	h.ID = ""
	return h, err
}

// lpInterruptDeploy starts a release and ends the loop's context while the deploy command runs,
// as a daemon shutdown does. It returns after that loop call has returned, with the plain
// deployer back in place for the "restarted" daemon.
func lpInterruptDeploy(t *testing.T, env *lpEnv) {
	t.Helper()
	entered := make(chan struct{})
	env.e.d.DeployerFor = func(*config.Project) (deploy.Deployer, error) {
		return lpBlockingDeployer{lpDeployer: env.dep, entered: entered}, nil
	}
	ctx, cancel := context.WithCancel(env.ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = env.e.RunLoop(ctx, loopDeployWatch, "demo") // a cancelled tick may report an error
	}()
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("deploy command never started")
	}
	if rel := lpDeployments(t, env)[0]; rel.State != domain.DeployDeploying || rel.ProviderID != "" {
		t.Errorf("release = %s (provider %q) while the deploy command runs, want deploying with no provider ID", rel.State, rel.ProviderID)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("loop did not return after its context ended")
	}
	env.e.d.DeployerFor = func(*config.Project) (deploy.Deployer, error) { return env.dep, nil }
}

// lpPreviousHealthy records an older healthy release to roll back to.
func lpPreviousHealthy(t *testing.T, env *lpEnv) domain.Deployment {
	t.Helper()
	prev, err := env.st.CreateDeployment(env.ctx, domain.Deployment{
		Project: "demo", Ref: "prev-sha", ProviderID: "prov-old", State: domain.DeployHealthy,
		StartedAt: env.now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	return prev
}

// lpDeployCount is how many times the deployer's Deploy was called.
func lpDeployCount(env *lpEnv) int {
	env.dep.mu.Lock()
	defer env.dep.mu.Unlock()
	return len(env.dep.deployed)
}

// lpRollbacks is a copy of the rollbacks the deployer was asked to do.
func lpRollbacks(env *lpEnv) [][2]string {
	env.dep.mu.Lock()
	defer env.dep.mu.Unlock()
	return append([][2]string(nil), env.dep.rolledBack...)
}

func TestLoopDeployRestartBeforeCommandStartedFailsWithoutRunningIt(t *testing.T) {
	health := lpNewHealth(t)
	env := lpNewEngine(t, lpDeployDoc(t, health.srv.URL))
	lpPreviousHealthy(t, env)
	merged := lpMergedTask(t, env, "Add notes", "sha-1")
	// What a crash between recording the release and starting its command leaves behind.
	if _, err := env.st.CreateDeployment(env.ctx, domain.Deployment{
		Project: "demo", TaskID: merged.ID, Ref: "sha-1", State: domain.DeployPending,
		StartedAt: env.now, UpdatedAt: env.now,
	}); err != nil {
		t.Fatal(err)
	}

	env.run(loopDeployWatch)

	rel := lpDeployments(t, env)[0]
	if rel.State != domain.DeployFailed {
		t.Fatalf("release = %s, want failed", rel.State)
	}
	if rel.Notes != "deploy was interrupted before it started" {
		t.Fatalf("notes = %q, want the interrupted-before-start reason", rel.Notes)
	}
	if n := lpDeployCount(env); n != 0 {
		t.Fatalf("the deploy command ran %d times for a release that never started, want 0", n)
	}
	if rb := lpRollbacks(env); len(rb) != 0 {
		t.Fatalf("rollbacks = %v, want none", rb)
	}
	if got := env.task(merged.ID); got.Status != domain.TaskBlocked {
		t.Fatalf("task = %s, want blocked", got.Status)
	}
}

func TestLoopDeployRestartMidCommandKeepsHealthyRelease(t *testing.T) {
	health := lpNewHealth(t)
	env := lpNewEngine(t, lpDeployDoc(t, health.srv.URL))
	lpPreviousHealthy(t, env)
	merged := lpMergedTask(t, env, "Add notes", "sha-1")

	lpInterruptDeploy(t, env)
	env.run(loopDeployWatch) // the restarted daemon's first tick

	rel := lpDeployments(t, env)[0]
	if rel.State != domain.DeployVerifying {
		t.Fatalf("release = %s after restart, want verifying", rel.State)
	}
	if rel.VerifyingSince == nil || !rel.VerifyingSince.Equal(env.now) {
		t.Fatalf("verifying since = %v, want %v", rel.VerifyingSince, env.now)
	}
	if !strings.Contains(rel.Notes, "interrupted") {
		t.Fatalf("notes = %q, want them to say the deploy was interrupted", rel.Notes)
	}
	if !strings.Contains(rel.Log, "== deploy interrupted ==") {
		t.Fatalf("log = %q, want a deploy interrupted marker", rel.Log)
	}
	if got := env.task(merged.ID); got.Status != domain.TaskDeploying {
		t.Fatalf("task = %s, want deploying", got.Status)
	}

	env.now = env.now.Add(11 * time.Minute)
	env.run(loopDeployWatch)

	rel = lpDeployments(t, env)[0]
	if rel.State != domain.DeployHealthy {
		t.Fatalf("release = %s after the health window, want healthy", rel.State)
	}
	if got := env.task(merged.ID); got.Status != domain.TaskDone {
		t.Fatalf("task = %s, want done", got.Status)
	}
	if n := lpDeployCount(env); n != 1 {
		t.Fatalf("the deploy command ran %d times for one release, want 1", n)
	}
	if rb := lpRollbacks(env); len(rb) != 0 {
		t.Fatalf("rollbacks = %v, want none", rb)
	}
}

func TestLoopDeployRestartMidCommandRollsBackUnhealthyReleaseOnce(t *testing.T) {
	health := lpNewHealth(t)
	env := lpNewEngine(t, lpDeployDoc(t, health.srv.URL))
	previous := lpPreviousHealthy(t, env)
	merged := lpMergedTask(t, env, "Break things", "sha-2")

	lpInterruptDeploy(t, env)
	health.code.Store(500)

	env.run(loopDeployWatch) // restart tick: moves to verifying
	if rel := lpDeployments(t, env)[0]; rel.State != domain.DeployVerifying {
		t.Fatalf("release = %s after restart, want verifying", rel.State)
	}

	env.run(loopDeployWatch)
	env.run(loopDeployWatch)
	env.run(loopDeployWatch) // three failed checks in a row

	rel := lpDeployments(t, env)[0]
	if rel.State != domain.DeployRolledBack {
		t.Fatalf("release = %s, want rolled_back", rel.State)
	}
	if !strings.Contains(rel.Notes, "health check failing") {
		t.Fatalf("notes = %q, want the health reason", rel.Notes)
	}
	if !strings.Contains(rel.Notes, "rolled back to prev-sh") {
		t.Fatalf("notes = %q, want the rollback", rel.Notes)
	}

	env.run(loopDeployWatch)
	env.run(loopDeployWatch)

	rb := lpRollbacks(env)
	if len(rb) != 1 || rb[0] != [2]string{previous.ProviderID, previous.Ref} {
		t.Fatalf("rollbacks = %v, want exactly one, to the previous healthy release", rb)
	}
	if n := lpDeployCount(env); n != 1 {
		t.Fatalf("the deploy command ran %d times for one release, want 1", n)
	}
	if got := env.task(merged.ID); got.Status != domain.TaskBlocked {
		t.Fatalf("task = %s, want blocked", got.Status)
	}
	incidents := env.tasks(domain.KindIncident)
	if len(incidents) != 1 {
		t.Fatalf("incidents = %d, want 1", len(incidents))
	}
	if !strings.Contains(incidents[0].Body, "== deploy interrupted ==") {
		t.Fatalf("incident body = %q, does not say the deploy was interrupted", incidents[0].Body)
	}
}

func TestLoopDeployRestartMidCommandWithoutHealthURLRollsBack(t *testing.T) {
	env := lpNewEngine(t, lpDeployDoc(t, ""))
	previous := lpPreviousHealthy(t, env)
	merged := lpMergedTask(t, env, "Add notes", "sha-3")

	lpInterruptDeploy(t, env)
	env.run(loopDeployWatch)

	rel := lpDeployments(t, env)[0]
	if rel.State != domain.DeployRolledBack {
		t.Fatalf("release = %s, want rolled_back", rel.State)
	}
	if !strings.Contains(rel.Notes, "without a health_url") {
		t.Fatalf("notes = %q, want the no-health-URL reason", rel.Notes)
	}
	if !strings.Contains(rel.Notes, "rolled back to prev-sh") {
		t.Fatalf("notes = %q, want the rollback", rel.Notes)
	}
	rb := lpRollbacks(env)
	if len(rb) != 1 || rb[0] != [2]string{previous.ProviderID, previous.Ref} {
		t.Fatalf("rollbacks = %v, want exactly one, to the previous healthy release", rb)
	}
	if n := lpDeployCount(env); n != 1 {
		t.Fatalf("the deploy command ran %d times for one release, want 1", n)
	}
	if got := env.task(merged.ID); got.Status != domain.TaskBlocked {
		t.Fatalf("task = %s, want blocked", got.Status)
	}
}

func TestLoopDeployHandleWithoutIDIsNotTakenForInterrupted(t *testing.T) {
	health := lpNewHealth(t)
	env := lpNewEngine(t, lpDeployDoc(t, health.srv.URL))
	env.e.d.DeployerFor = func(*config.Project) (deploy.Deployer, error) {
		return lpNoIDDeployer{env.dep}, nil
	}
	lpMergedTask(t, env, "Add notes", "sha-4")

	env.run(loopDeployWatch)

	rel := lpDeployments(t, env)[0]
	if rel.State != domain.DeployDeploying {
		t.Fatalf("release = %s, want deploying", rel.State)
	}
	if rel.ProviderID != rel.ID {
		t.Fatalf("provider ID = %q, want the deployment's own ID %q when the deployer gives none", rel.ProviderID, rel.ID)
	}

	env.dep.status = deploy.Status{State: deploy.StateReady}
	env.run(loopDeployWatch)

	rel = lpDeployments(t, env)[0]
	if rel.State != domain.DeployVerifying || rel.Notes != "" {
		t.Fatalf("release = %s notes %q, want verifying with no notes (a finished deploy was treated as interrupted)", rel.State, rel.Notes)
	}
}

// A crash between marking a release verifying and moving its task to deploying leaves the task
// merged. The healthy release must still finish it, or the next tick would deploy it again.
func TestLoopDeployHealthyReleaseFinishesTaskStillMerged(t *testing.T) {
	health := lpNewHealth(t)
	env := lpNewEngine(t, lpDeployDoc(t, health.srv.URL))
	merged := lpMergedTask(t, env, "Add notes", "sha-1")
	since := env.now
	if _, err := env.st.CreateDeployment(env.ctx, domain.Deployment{
		Project: "demo", TaskID: merged.ID, Ref: "sha-1", ProviderID: "prov-x",
		State: domain.DeployVerifying, VerifyingSince: &since, StartedAt: env.now, UpdatedAt: env.now,
	}); err != nil {
		t.Fatal(err)
	}

	env.now = env.now.Add(11 * time.Minute)
	env.run(loopDeployWatch)

	if rel := lpDeployments(t, env)[0]; rel.State != domain.DeployHealthy {
		t.Fatalf("release = %s, want healthy", rel.State)
	}
	if got := env.task(merged.ID); got.Status != domain.TaskDone {
		t.Fatalf("task = %s after its release turned healthy, want done", got.Status)
	}
	env.run(loopDeployWatch)
	if n := lpDeployCount(env); n != 0 {
		t.Fatalf("the deploy command ran %d times for an already healthy release, want 0", n)
	}
	if n := len(lpDeployments(t, env)); n != 1 {
		t.Fatalf("got %d releases, want 1", n)
	}
}
