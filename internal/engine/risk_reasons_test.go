package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/policy"
)

func TestReviewerReasonsAreFoundUnderTheLabel(t *testing.T) {
	got := reviewerReasons([]string{
		"agent rated risk high: touches tokens; reviewer: changes the gate",
		"reviewer: drops a column",
		"reviewer: drops a column",
		"old worker reason",
		"agent rated risk high: agent rated risk high: x; reviewer: nested",
	})
	assert.Equal(t, []string{"reviewer: changes the gate", "reviewer: drops a column", "reviewer: nested"}, got)
	assert.Empty(t, reviewerReasons(nil))
}

func TestDecideRiskKeepsEveryReviewerReason(t *testing.T) {
	pol := config.Policy{Rules: config.DefaultRules(), Gates: config.DefaultGates()}
	in := policy.Input{Kind: domain.KindFeature, Files: []policy.FileChange{{Path: "migrations/0001.sql", Added: 1}}}
	orders := [][]string{
		{"reviewer: touches auth and payments", "reviewer: touches auth"},
		{"reviewer: touches auth", "reviewer: touches auth and payments"},
	}
	for _, reasons := range orders {
		task := domain.Task{Risk: domain.RiskHigh, RiskReasons: reasons}
		d := decideRisk(pol, in, task)
		assert.Contains(t, d.Reasons, "reviewer: touches auth and payments")
		assert.Contains(t, d.Reasons, "reviewer: touches auth")

		task.Risk, task.RiskReasons = d.Risk, d.Reasons
		again := decideRisk(pol, in, task)
		assert.Equal(t, d.Reasons, again.Reasons)
	}
}

func TestRaiseForReviewerDoesNotRepeatAFoldedReason(t *testing.T) {
	task := domain.Task{Risk: domain.RiskHigh, RiskReasons: []string{"agent rated risk high: touches tokens; reviewer: changes the gate"}}
	raiseForReviewer(&task, domain.RiskHigh, []string{"changes the gate", "new point"})
	assert.Equal(t, []string{"agent rated risk high: touches tokens; reviewer: changes the gate", "reviewer: new point"}, task.RiskReasons)
}

func TestCarryRiskKeepsOnlyTheReviewersPart(t *testing.T) {
	task := domain.Task{Risk: domain.RiskMedium, RiskReasons: []string{"agent rated risk medium: old worker reason; reviewer: schema change"}}
	carryRisk(&task, domain.RiskHigh, []string{"rewrites auth"})
	assert.Equal(t, domain.RiskHigh, task.Risk)
	assert.Equal(t, []string{"rewrites auth", "reviewer: schema change"}, task.RiskReasons)
}

func TestReasonPartsKeepsAnUnlabelledReasonWhole(t *testing.T) {
	assert.Equal(t, []string{"reviewer: a; b"}, reasonParts("reviewer: a; b"))
	assert.Equal(t, []string{"a", "b"}, reasonParts("agent rated risk high: a; b"))
	assert.Len(t, reasonParts("agent rated risk high"), 0)
	assert.Empty(t, reasonParts(""), "an empty reason holds no reasons")
}
