package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/bobbyrc/onett/internal/domain"
)

func (s *suEnv) loop() {
	s.t.Helper()
	if err := s.e.selfUpdateLoop(s.ctx, s.e.Config().Projects["demo"]); err != nil {
		s.t.Fatal(err)
	}
}

// Agent runs survive a restart, so a live task run and queued work no longer hold the updater
// back, and the factory is never paused for it.
func TestSelfUpdateStartsDespiteLiveRunAndQueuedTasks(t *testing.T) {
	s := suNew(t)
	if _, err := s.st.CreateRun(s.ctx, domain.Run{
		TaskID: "task-1", Project: "demo", Role: "implementer", Status: domain.RunRunning, StartedAt: s.now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.CreateTask(context.Background(), domain.Task{
		Project: "demo", Kind: domain.KindFeature, Title: "wait", Status: domain.TaskQueued, MaxAttempts: 2,
	}); err != nil {
		t.Fatal(err)
	}
	s.loop()
	if n := s.started(); n != 1 {
		t.Fatalf("started %d updaters, want 1", n)
	}
	if s.e.paused.Load() || s.e.PauseInfo() != nil {
		t.Fatal("the factory was paused for the update")
	}
	if env := strings.Join(s.cmds[0].Env, "\n"); strings.Contains(env, "ONETT_UPDATE_WAIT") {
		t.Fatal("the updater is still given ONETT_UPDATE_WAIT")
	}
}

// A deploy command is a child of the daemon, so a restart would cut it off: no updater while any
// project has a release in flight, and it starts on the next tick once the release settles.
func TestSelfUpdateWaitsForReleaseInFlight(t *testing.T) {
	s := suNew(t)
	other := domain.Deployment{
		Project: "other", Ref: "dddddddddddddddddddddddddddddddddddddddd", State: domain.DeployDeploying,
		StartedAt: s.now, UpdatedAt: s.now,
	}
	d, err := s.st.CreateDeployment(s.ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	s.loop()
	if n := s.started(); n != 0 {
		t.Fatalf("started %d updaters during a release", n)
	}
	if _, err := s.st.UpdateDeployment(s.ctx, d.ID, func(x *domain.Deployment) error {
		x.State = domain.DeployHealthy
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.loop()
	if n := s.started(); n != 1 {
		t.Fatalf("started %d updaters after the release settled, want 1", n)
	}
}
