// Tests for the process ID file kept while a local deploy command runs.
package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/deploy"
	"github.com/bobbyrc/onett/internal/domain"
)

// lpProcessDeployer is lpDeployer as a deploy.ProcessDeployer: it reports the test's own process
// ID as the command's, and records what PID files exist in the data directory at that moment.
type lpProcessDeployer struct {
	*lpDeployer
	dataDir string
	during  *[]string // "name=content" of each deploy-*.pid file while the command runs
}

func (d lpProcessDeployer) DeployProcess(ctx context.Context, ref string, started func(pid int)) (deploy.Handle, error) {
	started(os.Getpid())
	files, _ := filepath.Glob(filepath.Join(d.dataDir, "deploy-*.pid"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		*d.during = append(*d.during, filepath.Base(f)+"="+string(b))
	}
	return d.lpDeployer.Deploy(ctx, ref)
}

func TestLoopDeployKeepsCommandPIDFileWhileCommandRuns(t *testing.T) {
	health := lpNewHealth(t)
	env := lpNewEngine(t, lpDeployDoc(t, health.srv.URL))
	lpMergedTask(t, env, "Add notes", "sha-1")
	dataDir := env.e.Config().Server.DataDir
	var during []string
	env.e.d.DeployerFor = func(*config.Project) (deploy.Deployer, error) {
		return lpProcessDeployer{lpDeployer: env.dep, dataDir: dataDir, during: &during}, nil
	}

	env.run(loopDeployWatch)

	rel := lpDeployments(t, env)[0]
	want := []string{"deploy-" + rel.ID + ".pid=" + strconv.Itoa(os.Getpid()) + "\n"}
	if len(during) != 1 || during[0] != want[0] {
		t.Fatalf("pid files while the command ran = %q, want %q", during, want)
	}
	if files, _ := filepath.Glob(filepath.Join(dataDir, "deploy-*.pid")); len(files) != 0 {
		t.Fatalf("pid files left after the command returned: %v", files)
	}
	if rel.State != domain.DeployDeploying || rel.ProviderID != "prov-1" {
		t.Fatalf("release = %s (provider %q), want deploying with provider prov-1", rel.State, rel.ProviderID)
	}
}

// lpSeedInterrupted records a release whose deploy command started but never reported a result.
func lpSeedInterrupted(t *testing.T, env *lpEnv, taskID string) domain.Deployment {
	t.Helper()
	rel, err := env.st.CreateDeployment(env.ctx, domain.Deployment{
		Project: "demo", TaskID: taskID, Ref: "sha-1", State: domain.DeployDeploying,
		StartedAt: env.now, UpdatedAt: env.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

func TestLoopDeployRestartWaitsForOrphanedDeployCommand(t *testing.T) {
	health := lpNewHealth(t)
	env := lpNewEngine(t, lpDeployDoc(t, health.srv.URL))
	lpPreviousHealthy(t, env)
	merged := lpMergedTask(t, env, "Add notes", "sha-1")
	rel := lpSeedInterrupted(t, env, merged.ID)
	// The orphaned command: this test's own process, which is certainly alive.
	if err := os.WriteFile(env.e.deployPIDFile(rel.ID), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}

	env.run(loopDeployWatch)
	env.run(loopDeployWatch)

	if got := lpDeployments(t, env)[0]; got.State != domain.DeployDeploying {
		t.Fatalf("release = %s while its command is alive, want deploying", got.State)
	}
	if got := env.task(merged.ID); got.Status != domain.TaskMerged {
		t.Fatalf("task = %s while its command is alive, want merged", got.Status)
	}
	if n := lpDeployCount(env); n != 0 {
		t.Fatalf("the deploy command ran %d times, want 0", n)
	}

	env.now = env.now.Add(31 * time.Minute) // past the default 30m deploy timeout
	env.run(loopDeployWatch)

	if got := lpDeployments(t, env)[0]; got.State != domain.DeployVerifying {
		t.Fatalf("release = %s after the timeout, want verifying", got.State)
	}
	if _, err := os.Stat(env.e.deployPIDFile(rel.ID)); !os.IsNotExist(err) {
		t.Fatalf("pid file still present after the timeout: %v", err)
	}
	if n := lpDeployCount(env); n != 0 {
		t.Fatalf("the deploy command ran %d times, want 0", n)
	}
}

func TestLoopDeployRestartJudgesAtOnceWhenOrphanedCommandHasExited(t *testing.T) {
	health := lpNewHealth(t)
	env := lpNewEngine(t, lpDeployDoc(t, health.srv.URL))
	lpPreviousHealthy(t, env)
	merged := lpMergedTask(t, env, "Add notes", "sha-1")
	rel := lpSeedInterrupted(t, env, merged.ID)
	// A PID of a process that has already exited and been reaped.
	c := exec.Command("true")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.e.deployPIDFile(rel.ID), []byte(strconv.Itoa(c.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}

	env.run(loopDeployWatch)

	if got := lpDeployments(t, env)[0]; got.State != domain.DeployVerifying {
		t.Fatalf("release = %s, want verifying", got.State)
	}
	if got := env.task(merged.ID); got.Status != domain.TaskDeploying {
		t.Fatalf("task = %s, want deploying", got.Status)
	}
	if _, err := os.Stat(env.e.deployPIDFile(rel.ID)); !os.IsNotExist(err) {
		t.Fatalf("pid file still present: %v", err)
	}
	if n := lpDeployCount(env); n != 0 {
		t.Fatalf("the deploy command ran %d times, want 0", n)
	}
}
