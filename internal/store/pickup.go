package store

import (
	"sort"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
)

// PickupGroup is a task's place in the pickup-order listing: work happening now first, then
// what is next, then what is finished.
type PickupGroup int

const (
	GroupRunning    PickupGroup = iota // an agent is working on it
	GroupInProgress                    // review, CI, awaiting approval, merged, deploying
	GroupQueued                        // queued and claimable
	GroupWaiting                       // queued, but waiting on other tasks to land
	GroupBlocked
	GroupFinished // done, failed, cancelled
)

// GroupOf returns the pickup group of t.
func GroupOf(t domain.Task) PickupGroup {
	switch t.Status {
	case domain.TaskRunning:
		return GroupRunning
	case domain.TaskReview, domain.TaskAwaitingApproval, domain.TaskMerged, domain.TaskDeploying:
		return GroupInProgress
	case domain.TaskQueued:
		if len(t.WaitingOn) > 0 {
			return GroupWaiting
		}
		return GroupQueued
	case domain.TaskBlocked:
		return GroupBlocked
	}
	return GroupFinished
}

// QueuePos is where a queued task stands. Overall counts every claimable queued task in the
// scheduler's claim order; InProject counts only those of the same project. The scheduler
// claims one task at a time but skips projects at their run limit, so InProject is the honest
// number when a project is busy.
type QueuePos struct {
	Overall   int
	InProject int
}

// ClaimLess is the scheduler's claim order (ClaimNextTask): priority, then oldest, then id.
func ClaimLess(a, b domain.Task) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.ID < b.ID
}

// PickupOrder sorts tasks in place into pickup order: running (by start time, oldest first;
// startedAt maps task id to its live run's start, falling back to UpdatedAt), in progress and
// blocked (by last update, oldest first), queued in claim order, waiting in claim order, and
// finished most recent first. It returns the queue position of each claimable queued task.
func PickupOrder(tasks []domain.Task, startedAt map[string]time.Time) map[string]QueuePos {
	start := func(t domain.Task) time.Time {
		if s, ok := startedAt[t.ID]; ok {
			return s
		}
		return t.UpdatedAt
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		ga, gb := GroupOf(a), GroupOf(b)
		if ga != gb {
			return ga < gb
		}
		switch ga {
		case GroupRunning:
			if sa, sb := start(a), start(b); !sa.Equal(sb) {
				return sa.Before(sb)
			}
		case GroupInProgress, GroupBlocked:
			if !a.UpdatedAt.Equal(b.UpdatedAt) {
				return a.UpdatedAt.Before(b.UpdatedAt)
			}
		case GroupQueued, GroupWaiting:
			return ClaimLess(a, b)
		case GroupFinished:
			if !a.UpdatedAt.Equal(b.UpdatedAt) {
				return a.UpdatedAt.After(b.UpdatedAt)
			}
		}
		return a.ID < b.ID
	})
	pos := map[string]QueuePos{}
	perProject := map[string]int{}
	n := 0
	for _, t := range tasks {
		if GroupOf(t) != GroupQueued {
			continue
		}
		n++
		perProject[t.Project]++
		pos[t.ID] = QueuePos{Overall: n, InProject: perProject[t.Project]}
	}
	return pos
}
