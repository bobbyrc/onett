package engine

import (
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestLoopsViewCarriesNextRunAndItemCount(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	next := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	if err := env.st.SaveLoopState(env.ctx, domain.LoopState{
		Name: loopHeartbeat, Project: "demo", LastRunAt: next.Add(-time.Hour), NextRunAt: &next, ItemCount: 3,
	}); err != nil {
		t.Fatal(err)
	}
	for _, r := range env.e.Loops(env.ctx) {
		if r.Name == loopHeartbeat && r.Project == "demo" {
			if r.NextRunAt == nil || !r.NextRunAt.Equal(next) || r.ItemCount != 3 {
				t.Fatalf("heartbeat row = %+v, want next_run_at %v and item_count 3", r, next)
			}
			return
		}
	}
	t.Fatal("no heartbeat/demo row")
}
