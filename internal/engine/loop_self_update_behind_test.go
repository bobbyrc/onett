package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
)

// A release every 5 minutes must not keep pushing the update away: max_defer counts from the
// first eligible release.
func TestSelfUpdateMaxDeferCountsFromFirstEligibleRelease(t *testing.T) {
	s := suNew(t) // first release eligible 10 minutes ago
	s.liveRun("task-1")
	first := s.now.Add(-10 * time.Minute)
	maxDefer := s.e.Config().LoopsFor("demo").SelfUpdate.MaxDefer.Duration
	for i := 1; ; i++ {
		s.loop()
		if s.started() > 0 {
			break
		}
		if s.now.Sub(first) > maxDefer+time.Hour {
			t.Fatal("updater never started while releases kept landing")
		}
		s.now = s.now.Add(5 * time.Minute)
		s.deploy(fmt.Sprintf("%040d", i), domain.DeployHealthy, 20*time.Minute)
	}
	if got := s.now.Sub(first); got < maxDefer || got >= maxDefer+5*time.Minute {
		t.Fatalf("started %v after the first eligible release, want about %v", got, maxDefer)
	}
}
