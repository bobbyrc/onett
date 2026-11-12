package engine

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/policy"
	"github.com/bobbyrc/onett/internal/scm"
	"github.com/bobbyrc/onett/internal/store"
)

// lpCountEvents counts the events of type typ recorded so far.
func lpCountEvents(t *testing.T, env *lpEnv, typ string) int {
	t.Helper()
	evs, err := env.st.ListEvents(env.ctx, store.EventFilter{Limit: 10000})
	require.NoError(t, err)
	n := 0
	for _, ev := range evs {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

// lpRaiseBeforeUpdate raises a task's rating just before the next update of it, as a reviewer's
// verdict landing between the shepherd's read and its write would.
type lpRaiseBeforeUpdate struct {
	store.Store
	id    string
	fired bool
}

func (s *lpRaiseBeforeUpdate) UpdateTask(ctx context.Context, id string, fn func(*domain.Task) error) (domain.Task, error) {
	if id == s.id && !s.fired {
		s.fired = true
		if _, err := s.Store.UpdateTask(ctx, id, func(x *domain.Task) error {
			raiseForReviewer(x, domain.RiskHigh, []string{"changes the merge gate"})
			return nil
		}); err != nil {
			return domain.Task{}, err
		}
	}
	return s.Store.UpdateTask(ctx, id, fn)
}

func TestShepherdKeepsReviewerReasonsWhenRulesReachTheRisk(t *testing.T) {
	env, task := lpShepherdEnv(t, map[string]string{"migrations/0001_init.sql": "create table t();\n"})
	env.fake.SetCI(task.PRNumber, scm.CIPending)
	lpSetTask(t, env, task.ID, func(x *domain.Task) {
		raiseForReviewer(x, domain.RiskHigh, []string{"drops a column"})
	})

	env.run(loopPRShepherd)
	env.run(loopPRShepherd)

	got := env.task(task.ID)
	assert.Equal(t, domain.RiskHigh, got.Risk)
	assert.Equal(t, []string{"database schema or migration (migrations/0001_init.sql)", "reviewer: drops a column"}, got.RiskReasons)
}

func TestShepherdPassesOverUnchangedTaskChangeNothing(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	env.fake.SetCI(task.PRNumber, scm.CIPending)
	lpSetTask(t, env, task.ID, func(x *domain.Task) {
		x.Risk = domain.RiskHigh
		x.RiskReasons = []string{"touches session tokens", "reviewer: changes the merge gate"}
	})

	env.run(loopPRShepherd)

	first := env.task(task.ID)
	require.Equal(t, []string{"agent rated risk high: touches session tokens; reviewer: changes the merge gate"}, first.RiskReasons)
	assert.Equal(t, domain.RiskHigh, first.Risk)
	reasons := first.RiskReasons
	decisions := lpCountEvents(t, env, domain.EvPolicyDecision)
	updates := lpCountEvents(t, env, domain.EvTaskUpdated)

	for range 10 {
		env.run(loopPRShepherd)
	}

	got := env.task(task.ID)
	assert.Equal(t, reasons, got.RiskReasons)
	assert.Equal(t, domain.RiskHigh, got.Risk)
	assert.Equal(t, decisions, lpCountEvents(t, env, domain.EvPolicyDecision))
	assert.Equal(t, updates, lpCountEvents(t, env, domain.EvTaskUpdated))
}

func TestShepherdSettlesUnratedTask(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	env.fake.SetCI(task.PRNumber, scm.CIPending)
	lpSetTask(t, env, task.ID, func(x *domain.Task) {
		x.Risk = domain.RiskUnknown
		x.RiskReasons = nil
	})

	for range 3 {
		env.run(loopPRShepherd)
	}

	settled := env.task(task.ID)
	decisions := lpCountEvents(t, env, domain.EvPolicyDecision)
	updates := lpCountEvents(t, env, domain.EvTaskUpdated)

	for range 10 {
		env.run(loopPRShepherd)
	}

	got := env.task(task.ID)
	assert.Equal(t, settled.Risk, got.Risk)
	assert.Equal(t, settled.RiskReasons, got.RiskReasons)
	assert.Equal(t, decisions, lpCountEvents(t, env, domain.EvPolicyDecision))
	assert.Equal(t, updates, lpCountEvents(t, env, domain.EvTaskUpdated))
	for _, r := range got.RiskReasons {
		assert.LessOrEqual(t, strings.Count(r, "agent rated risk"), 1, "reason labelled twice: %q", r)
	}
}

func TestShepherdDoesNotPutBackARatingRaisedSinceItsRead(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	env.fake.SetCI(task.PRNumber, scm.CIPending)

	// Warm-up: records the PR head, so the next pass's first update of the task is the risk write.
	env.run(loopPRShepherd)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.RiskReasons = []string{"stale"} })

	w := &lpRaiseBeforeUpdate{Store: env.st, id: task.ID}
	env.e.d.Store = w
	env.run(loopPRShepherd)

	require.True(t, w.fired, "the reviewer's rating was never raised during the pass")
	got := env.task(task.ID)
	assert.Equal(t, domain.RiskHigh, got.Risk)
	assert.True(t, slices.ContainsFunc(got.RiskReasons, func(r string) bool {
		return strings.Contains(r, "reviewer: changes the merge gate")
	}), "reasons = %q", got.RiskReasons)

	env.run(loopPRShepherd)
	got = env.task(task.ID)
	assert.Equal(t, domain.RiskHigh, got.Risk)
	assert.True(t, slices.ContainsFunc(got.RiskReasons, func(r string) bool {
		return strings.Contains(r, "reviewer: changes the merge gate")
	}), "reasons = %q", got.RiskReasons)
}

func TestDecideRiskIsStable(t *testing.T) {
	pol := config.Policy{Rules: config.DefaultRules(), Gates: config.DefaultGates()}
	notes := []policy.FileChange{{Path: "notes.txt", Added: 1}}
	migration := []policy.FileChange{{Path: "migrations/0001.sql", Added: 1}}

	cases := []struct {
		name    string
		files   []policy.FileChange
		risk    domain.Risk
		reasons []string
		check   func(t *testing.T, final policy.Decision)
	}{
		{name: "low unrated reasons", files: notes, risk: domain.RiskLow},
		{name: "unknown risk", files: notes, risk: domain.RiskUnknown},
		{name: "high with a reason", files: notes, risk: domain.RiskHigh, reasons: []string{"touches tokens"}},
		{
			name: "old nested label", files: notes, risk: domain.RiskHigh,
			reasons: []string{"agent rated risk high: agent rated risk high: agent rated risk high: touches tokens"},
			check: func(t *testing.T, final policy.Decision) {
				assert.Equal(t, []string{"agent rated risk high: touches tokens"}, final.Reasons)
			},
		},
		{
			name: "reviewer reason on a rule-rated change", files: migration, risk: domain.RiskHigh,
			reasons: []string{"reviewer: drops a column"},
			check: func(t *testing.T, final policy.Decision) {
				assert.Contains(t, final.Reasons, "reviewer: drops a column")
			},
		},
		{name: "low with a reason on a rule-rated change", files: migration, risk: domain.RiskLow, reasons: []string{"tiny"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := domain.Task{Risk: tc.risk, RiskReasons: tc.reasons}
			in := policy.Input{Kind: domain.KindFeature, Files: tc.files}
			var ds []policy.Decision
			for range 5 {
				d := decideRisk(pol, in, task)
				if len(ds) > 0 {
					assert.GreaterOrEqual(t, d.Risk.Rank(), ds[len(ds)-1].Risk.Rank(), "risk went down")
				}
				ds = append(ds, d)
				task.Risk, task.RiskReasons = d.Risk, d.Reasons
			}
			final := ds[4]
			assert.Equal(t, ds[2].Risk, ds[3].Risk)
			assert.Equal(t, ds[2].Reasons, ds[3].Reasons)
			assert.Equal(t, ds[3].Risk, ds[4].Risk)
			assert.Equal(t, ds[3].Reasons, ds[4].Reasons)
			for _, r := range final.Reasons {
				assert.LessOrEqual(t, strings.Count(r, "agent rated risk"), 1, "reason labelled twice: %q", r)
			}
			if tc.check != nil {
				tc.check(t, final)
			}
		})
	}
}
