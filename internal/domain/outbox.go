package domain

import (
	"strconv"
	"strings"
)

// OutboxDir is the directory, relative to a run's worktree, where the agent writes its report.
// It is excluded from git through .git/info/exclude.
const OutboxDir = ".onett-run"

// ShotsDir is the directory under OutboxDir where the agent leaves screenshots, one subdirectory
// per label ("before", "after"). Onett publishes the PNGs; the agent cannot push.
const ShotsDir = "shots"

// ResultFile is the agent's report, written to OutboxDir/ResultFile before it finishes.
const ResultFile = "result.json"

type ResultStatus string

const (
	// ResultCompleted: the work is done; any code changes are committed on the branch.
	ResultCompleted ResultStatus = "completed"
	// ResultNoChanges: the task needed no code change (question answered, triage done, the bug
	// is not reproducible). Summary says why.
	ResultNoChanges ResultStatus = "no_changes"
	// ResultBlocked: the agent cannot continue without a human. BlockedReason says what it needs.
	ResultBlocked ResultStatus = "blocked"
	// ResultFailed: the agent tried and could not do it.
	ResultFailed ResultStatus = "failed"
)

// RunResult is the schema of result.json. Missing or unparsable result.json is treated as
// ResultFailed with the harness's final message as the summary.
type RunResult struct {
	Status        ResultStatus `json:"status"`
	Summary       string       `json:"summary"`
	BlockedReason string       `json:"blocked_reason,omitempty"`
	// Risk is the agent's own rating of its change. Policy may raise it, never lower it.
	Risk        Risk     `json:"risk,omitempty"`
	RiskReasons []string `json:"risk_reasons,omitempty"`
	PRTitle     string   `json:"pr_title,omitempty"`
	PRBody      string   `json:"pr_body,omitempty"`
	// Review is set only by reviewer runs.
	Review *ReviewVerdict `json:"review,omitempty"`
	// DependsOn names tasks whose unmerged work this run needs. A blocked run that sets it is
	// re-queued to wait for them instead of blocking the task.
	DependsOn []string         `json:"depends_on,omitempty"`
	Followups []FollowupTask   `json:"followups,omitempty"`
	Friction  []FrictionReport `json:"friction,omitempty"`
	// Finished: the agent says nothing more can come of its conversation. It closes the
	// conversation and changes nothing about the task.
	Finished bool `json:"finished,omitempty"`
}

type ReviewVerdict struct {
	Approve  bool            `json:"approve"`
	Findings []ReviewFinding `json:"findings,omitempty"`
}

type ReviewFinding struct {
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Severity string `json:"severity"` // "blocker", "major", "minor"
	Message  string `json:"message"`
}

// FollowupTask is new work the agent found but did not do. Onett queues it as a new task.
type FollowupTask struct {
	Title string   `json:"title"`
	Body  string   `json:"body"`
	Kind  TaskKind `json:"kind,omitempty"`
}

// IssueRef is an issue number as an agent wrote it. Agents get the type wrong ("224", "#224"),
// and one bad optional value must not void a whole report, so anything that is not a positive
// whole number decodes as 0: no issue named.
type IssueRef int

// UnmarshalJSON accepts a number, or a string holding one with an optional leading "#".
func (r *IssueRef) UnmarshalJSON(b []byte) error {
	s := strings.TrimPrefix(strings.TrimSpace(strings.Trim(string(b), `"`)), "#")
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		n = 0
	}
	*r = IssueRef(n)
	return nil
}

// FrictionReport is something that slowed the agent down.
type FrictionReport struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// Target is "project" (the repo being worked on) or "onett" (the factory itself).
	Target string `json:"target"`
	// Issue is the number of the open friction issue the agent says this report is another
	// sighting of; 0 when it names none. Onett checks the claim before it uses it.
	Issue IssueRef `json:"issue,omitempty"`
}
