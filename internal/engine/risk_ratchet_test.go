package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

func TestCarryRisk(t *testing.T) {
	cases := []struct {
		name        string
		stored      domain.Risk
		storedWhy   []string
		report      domain.Risk
		reportWhy   []string
		want        domain.Risk
		wantReasons []string
	}{
		{"unrated task takes the report", domain.RiskUnknown, nil, domain.RiskLow, []string{"tiny"},
			domain.RiskLow, []string{"tiny"}},
		{"higher stored rating stays with its reasons", domain.RiskHigh,
			[]string{"agent rated risk high: x; reviewer: touches the gate"}, domain.RiskLow, []string{"tiny"},
			domain.RiskHigh, []string{"agent rated risk high: x; reviewer: touches the gate"}},
		{"stored reviewer reason survives an unrated report", domain.RiskHigh,
			[]string{"reviewer: touches the gate"}, domain.RiskUnknown, nil,
			domain.RiskHigh, []string{"reviewer: touches the gate"}},
		{"higher report wins and keeps the reviewer's reason", domain.RiskMedium,
			[]string{"reviewer: schema change", "old worker reason"}, domain.RiskHigh, []string{"rewrites auth"},
			domain.RiskHigh, []string{"rewrites auth", "reviewer: schema change"}},
		{"equal report keeps the reviewer's reason", domain.RiskMedium,
			[]string{"reviewer: schema change"}, domain.RiskMedium, []string{"config only"},
			domain.RiskMedium, []string{"config only", "reviewer: schema change"}},
		{"unrated report does not replace a stored rating", domain.RiskMedium,
			[]string{"reviewer: schema change"}, domain.RiskUnknown, nil,
			domain.RiskMedium, []string{"reviewer: schema change"}},
		{"unrated report over a low rating is stored unrated, which policy reads as medium", domain.RiskLow,
			[]string{"old"}, domain.RiskUnknown, nil, domain.RiskUnknown, nil},
		{"low report replaces low stored", domain.RiskLow, []string{"old"}, domain.RiskLow, []string{"new"},
			domain.RiskLow, []string{"new"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := domain.Task{Risk: tc.stored, RiskReasons: tc.storedWhy}
			carryRisk(&task, tc.report, tc.reportWhy)
			assert.Equal(t, tc.want, task.Risk)
			assert.ElementsMatch(t, tc.wantReasons, task.RiskReasons)
		})
	}
}

func TestRaiseForReviewer(t *testing.T) {
	task := domain.Task{Risk: domain.RiskLow}
	raiseForReviewer(&task, domain.RiskHigh, []string{"a"})
	assert.Equal(t, domain.RiskHigh, task.Risk)
	require.NotEmpty(t, task.RiskReasons)
	assert.Equal(t, "reviewer: a", task.RiskReasons[len(task.RiskReasons)-1])

	raiseForReviewer(&task, domain.RiskHigh, []string{"a"})
	assert.Len(t, task.RiskReasons, 1, "the same reviewer reason is not added twice")

	low := domain.Task{Risk: domain.RiskHigh}
	raiseForReviewer(&low, domain.RiskLow, []string{"b"})
	assert.Equal(t, domain.RiskHigh, low.Risk)
	assert.Contains(t, low.RiskReasons, "reviewer: b")

	medium := domain.Task{Risk: domain.RiskMedium}
	raiseForReviewer(&medium, domain.RiskUnknown, []string{"c"})
	assert.Equal(t, domain.RiskMedium, medium.Risk)
	assert.Empty(t, medium.RiskReasons)

	unrated := domain.Task{}
	raiseForReviewer(&unrated, domain.RiskLow, nil)
	assert.Equal(t, domain.RiskMedium, unrated.Risk)
}

// reviewerRatesHigh is a script whose reviewer writes a verdict with a high risk rating. The
// implementer runs as the default fake.
func reviewerRatesHigh(approve bool) hfake.Script {
	return func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		if role := spec.Env["ONETT_ROLE"]; role != "code-reviewer" && role != "reviewer" {
			return hfake.DefaultScript(ctx, spec, emit)
		}
		verdict := &domain.ReviewVerdict{Approve: approve}
		if !approve {
			verdict.Findings = []domain.ReviewFinding{{Severity: "blocker", Message: "needs a test"}}
		}
		rr := domain.RunResult{Status: domain.ResultCompleted, Summary: "review", Review: verdict,
			Risk: domain.RiskHigh, RiskReasons: []string{"changes the merge gate"}}
		if err := hfake.WriteResult(spec.Workdir, rr); err != nil {
			return harness.Result{IsError: true}, err
		}
		emit(harness.Event{Kind: domain.LogText, Text: "review written", SessionID: "fake-" + spec.RunID})
		return harness.Result{SessionID: "fake-" + spec.RunID, FinalText: "review"}, nil
	}
}

func TestWorkerReportCannotLowerRejectingReviewersRisk(t *testing.T) {
	env := rtNewEngine(t, rtOpts{script: reviewerRatesHigh(false)})
	parent := env.waitStatus(env.create(domain.Task{Body: "[fake:commit]"}).ID, domain.TaskReview)
	require.Equal(t, domain.RiskLow, parent.Risk)

	review := env.create(domain.Task{
		Kind: domain.KindReview, Role: "reviewer", ParentID: parent.ID, Title: "Review " + parent.Title,
		Branch: parent.Branch, PRNumber: parent.PRNumber,
	})
	env.waitStatus(review.ID, domain.TaskDone)

	rtWait(t, 30*time.Second, "worker to report again", func() bool {
		return len(env.runs(parent.ID)) == 2 && env.task(parent.ID).Status == domain.TaskReview
	})
	got := env.task(parent.ID)
	assert.Equal(t, domain.RiskHigh, got.Risk)
	assert.Contains(t, got.RiskReasons, "reviewer: changes the merge gate")
}

func TestWorkerReportCannotLowerApprovingReviewersRisk(t *testing.T) {
	env := rtNewEngine(t, rtOpts{script: reviewerRatesHigh(true)})
	parent := env.waitStatus(env.create(domain.Task{Body: "[fake:commit]"}).ID, domain.TaskReview)

	review := env.create(domain.Task{
		Kind: domain.KindReview, Role: "reviewer", ParentID: parent.ID, Title: "Review " + parent.Title,
		Branch: parent.Branch, PRNumber: parent.PRNumber,
	})
	env.waitStatus(review.ID, domain.TaskDone)
	rtWait(t, 30*time.Second, "approval recorded", func() bool {
		return env.task(parent.ID).ApprovedSHA != ""
	})
	require.Equal(t, domain.RiskHigh, env.task(parent.ID).Risk)

	// A CI failure sends the parent back to its worker, whose new report rates it low.
	env.e.requeueOrBlock(env.ctx, parent.ID, "CI failed: test", "")
	rtWait(t, 30*time.Second, "worker to report again", func() bool {
		return len(env.runs(parent.ID)) == 2 && env.task(parent.ID).Status == domain.TaskReview
	})
	got := env.task(parent.ID)
	assert.Equal(t, domain.RiskHigh, got.Risk)
	assert.Contains(t, got.RiskReasons, "reviewer: changes the merge gate")
}
