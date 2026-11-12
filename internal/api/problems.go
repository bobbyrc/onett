package api

import (
	"context"
	"net/http"
	"time"
)

// Problem kinds.
const (
	ProblemBlockedTasks = "blocked_tasks"
	ProblemFailingLoops = "failing_loops"
	ProblemBlockedGoal  = "blocked_goal"
)

// Problem is one thing that is wrong in a project, as GET /problems reports it: the tasks that
// share one cause of a block, the project's failing loops, or a blocked goal. Title and Detail
// quote agents and tools, so they are redacted, one line and bounded (see the engine).
type Problem struct {
	Kind    string `json:"kind"`
	Project string `json:"project"`
	// Key is unique in one response and stays the same while the problem lasts.
	Key    string `json:"key"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// Count is the number of blocked tasks, the longest run of failed passes among the loops,
	// or 1 for a goal.
	Count int       `json:"count"`
	Since time.Time `json:"since"`
	// LastAt is the latest block or the latest failed pass. Goals have none.
	LastAt *time.Time `json:"last_at,omitempty"`
	// ToldAt is when onett last notified the owner about the problem after it stayed open (the
	// heartbeat's escalation or the loop alert). Nil when it has not.
	ToldAt *time.Time    `json:"told_at,omitempty"`
	Tasks  []ProblemTask `json:"tasks,omitempty"`
	Loops  []ProblemLoop `json:"loops,omitempty"`
	GoalID string        `json:"goal_id,omitempty"`
}

// ProblemTask is one blocked task of a Problem.
type ProblemTask struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Attempts    int    `json:"attempts"`
	MaxAttempts int    `json:"max_attempts"`
}

// ProblemLoop is one failing loop of a Problem: how many passes failed in a row, and since when.
type ProblemLoop struct {
	Name  string    `json:"name"`
	Fails int       `json:"fails"`
	Since time.Time `json:"since"`
}

// ProblemLister is the part of Controller that GET /problems needs.
type ProblemLister interface {
	// Problems returns what is wrong in every configured project, one entry per cause, sorted by
	// project, then kind (blocked tasks, failing loops, blocked goals), then size and age.
	Problems(ctx context.Context) ([]Problem, error)
}

func (s *server) listProblems(w http.ResponseWriter, r *http.Request) {
	out, err := s.opts.Controller.Problems(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(out))
}
