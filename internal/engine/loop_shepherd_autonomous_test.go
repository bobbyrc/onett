package engine

import (
	"testing"

	"github.com/bobbyrc/onett/internal/domain"
)

// Under the built-in default policy a high-risk PR merges once approved and green, and its risk
// is still recorded.
func TestLoopShepherdDefaultPolicyAutoMergesHighRiskAndRecordsRisk(t *testing.T) {
	env, task := lpShepherdEnv(t, map[string]string{"migrations/0001_init.sql": "create table t();\n"})
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })

	env.run(loopPRShepherd)

	got := env.task(task.ID)
	if got.Status != domain.TaskMerged || got.Risk != domain.RiskHigh {
		t.Fatalf("task = %s risk %s, want merged with risk high recorded", got.Status, got.Risk)
	}
	if len(env.fake.Merged) != 1 || env.fake.Merged[0] != task.PRNumber {
		t.Fatalf("fake merged %v, want [%d]", env.fake.Merged, task.PRNumber)
	}
	pending, err := env.st.ListApprovals(env.ctx, domain.ApprovalPending)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending approvals = %+v (err %v), want none", pending, err)
	}
}

// The supervised policy still merges low-risk changes by itself.
func TestLoopShepherdSupervisedAutoMergesLowRisk(t *testing.T) {
	env, task := lpShepherdEnvPolicy(t, lpNotesFile, "supervised")
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })

	env.run(loopPRShepherd)

	if got := env.task(task.ID); got.Status != domain.TaskMerged {
		t.Fatalf("status = %s, want merged", got.Status)
	}
}
