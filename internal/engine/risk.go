package engine

import (
	"slices"
	"strings"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/policy"
)

// reviewerReasonPrefix marks a risk reason as the reviewer's.
const reviewerReasonPrefix = "reviewer: "

// reasonParts returns the single reasons a stored reason holds. A reason under the policy's label
// ("agent rated risk high: a; b") holds the reasons folded there; any other reason is one reason,
// whatever punctuation it has. A label with nothing under it, or an empty reason, holds none.
func reasonParts(r string) []string {
	under := unwrapAgentReason(r)
	if under == "" {
		return nil
	}
	if under == r {
		return []string{r}
	}
	return strings.Split(under, "; ")
}

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

// reviewerReasons returns the reviewer's reasons among the stored ones, each once. The shepherd's
// decision folds the stored reasons under one label ("agent rated risk high: a; reviewer: b"), so
// they are looked for under that label too, and matched whole: one reviewer reason that is the
// start of another is still its own reason.
func reviewerReasons(reasons []string) []string {
	var out []string
	for _, r := range reasons {
		for _, part := range reasonParts(r) {
			if strings.HasPrefix(part, reviewerReasonPrefix) && !slices.Contains(out, part) {
				out = append(out, part)
			}
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
	have := reviewerReasons(x.RiskReasons)
	for _, why := range reasons {
		if r := reviewerReasonPrefix + why; !slices.Contains(have, r) {
			x.RiskReasons = append(x.RiskReasons, r)
			have = append(have, r)
		}
	}
}

// agentRatedLabel starts the reason policy.Evaluate gives when an agent's rating is the highest:
// "agent rated risk high: <the agent's reasons>".
const agentRatedLabel = "agent rated risk "

// unwrapAgentReason takes the policy's label off a reason, however often it was put on, so that
// the reasons under it can be given to the policy again without being labelled twice. A label
// with nothing under it gives "".
func unwrapAgentReason(r string) string {
	for {
		rest, ok := strings.CutPrefix(r, agentRatedLabel)
		if !ok {
			return r
		}
		_, r, _ = strings.Cut(rest, ": ")
	}
}

// decideRisk applies pol to the change of task t. The stored rating goes in as the agent's
// rating, so the decision is never lower than what is stored. The stored reasons are a mix of
// what agents said and what an earlier decision wrote, and giving the latter back to the policy
// made every pass label them again. So only the agents' part goes in: the stored reasons that
// the policy does not give by itself, with an earlier label taken off. A reviewer's reasons that
// the policy leaves out, because its rules alone reach the rating, are added back. The result
// settles: a task that holds a settled decision gets the same risk and reasons from the next
// call. A task whose agent gave no rating takes two calls, because its first decision ("agent did
// not rate its risk") is put under the label once.
func decideRisk(pol config.Policy, in policy.Input, t domain.Task) policy.Decision {
	in.AgentRisk, in.AgentRiskReasons = t.Risk, nil
	own := policy.Evaluate(pol, in).Reasons
	var said []string
	for _, r := range t.RiskReasons {
		if slices.Contains(own, r) {
			continue
		}
		said = append(said, reasonParts(r)...)
	}
	in.AgentRiskReasons = said
	d := policy.Evaluate(pol, in)
	have := reviewerReasons(d.Reasons)
	for _, r := range reviewerReasons(said) {
		if !slices.Contains(have, r) {
			d.Reasons = append(d.Reasons, r)
		}
	}
	return d
}

// storeDecision returns the shepherd's write of decision d on a task it read as read. The write
// is skipped when the task has been rated again since, by a reviewer's verdict for example: the
// decision was made without that rating, so the next pass decides again.
func storeDecision(read domain.Task, d policy.Decision) func(*domain.Task) error {
	return func(x *domain.Task) error {
		if x.Risk != read.Risk || !slices.Equal(x.RiskReasons, read.RiskReasons) {
			return errSkip
		}
		x.Risk, x.RiskReasons = d.Risk, d.Reasons
		return nil
	}
}
