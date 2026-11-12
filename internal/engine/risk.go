package engine

import (
	"slices"
	"strings"

	"github.com/bobbyrc/onett/internal/domain"
)

// reviewerReasonPrefix marks a risk reason as the reviewer's.
const reviewerReasonPrefix = "reviewer: "

// carryRisk records the rating of a worker's new report on task x without lowering what is
// stored. Risk only goes up: the stored rating may be the reviewer's or an earlier policy
// decision, and the worker's own text must not undercut either. A stored rating that is higher
// stays, with the reasons that explain it. Otherwise the report's rating and reasons are stored
// and the reviewer's reasons are carried over. An unrated report counts as medium, as policy
// counts it, and never replaces a rating.
func carryRisk(x *domain.Task, risk domain.Risk, reasons []string) {
	reported := risk
	if reported == domain.RiskUnknown {
		reported = domain.RiskMedium
	}
	rated := x.Risk != domain.RiskUnknown
	if rated && x.Risk.Rank() > reported.Rank() {
		return
	}
	if !rated || reported.Rank() > x.Risk.Rank() {
		x.Risk = risk
	}
	x.RiskReasons = append(slices.Clone(reasons), reviewerReasons(x.RiskReasons)...)
}

// reviewerReasons returns the stored reasons that carry a reviewer's reason. The shepherd folds
// the stored reasons into one "agent rated risk high: a; reviewer: b" reason when it applies the
// policy, so the mark is looked for anywhere in a reason, not only at its start.
func reviewerReasons(reasons []string) []string {
	var out []string
	for _, r := range reasons {
		if strings.Contains(r, reviewerReasonPrefix) {
			out = append(out, r)
		}
	}
	return out
}

// raiseForReviewer merges a reviewer's rating into the reviewed task x: the higher of the two
// stands, and the reviewer's reasons are added, marked as the reviewer's. An unrated review
// changes nothing.
func raiseForReviewer(x *domain.Task, risk domain.Risk, reasons []string) {
	if risk == domain.RiskUnknown {
		return
	}
	base := x.Risk
	if base == domain.RiskUnknown {
		base = domain.RiskMedium
	}
	x.Risk = domain.MaxRisk(base, risk)
	for _, why := range reasons {
		if r := reviewerReasonPrefix + why; !slices.Contains(x.RiskReasons, r) {
			x.RiskReasons = append(x.RiskReasons, r)
		}
	}
}
