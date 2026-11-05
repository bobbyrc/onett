package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

var goals = entity[domain.Goal]{
	name:  "goal",
	table: "goals",
	id:    func(g *domain.Goal) *string { return &g.ID },
	stamp: func(g *domain.Goal, now time.Time) { g.UpdatedAt = now },
	cols: func(g domain.Goal) []col {
		return []col{
			{"id", g.ID},
			{"project", g.Project},
			{"status", string(g.Status)},
			{"created_at", nanos(g.CreatedAt)},
		}
	},
}

// CreateGoal stores g, assigning ID and timestamps when empty.
func (s *Store) CreateGoal(ctx context.Context, g domain.Goal) (domain.Goal, error) {
	if g.ID == "" {
		g.ID = newID()
	}
	now := nowUTC()
	if g.CreatedAt.IsZero() {
		g.CreatedAt = now
	}
	if g.UpdatedAt.IsZero() {
		g.UpdatedAt = now
	}
	if err := goals.insert(ctx, s.db, g); err != nil {
		return domain.Goal{}, fmt.Errorf("create goal: %w", err)
	}
	return g, nil
}

// GetGoal returns the goal with id, or ErrNotFound.
func (s *Store) GetGoal(ctx context.Context, id string) (domain.Goal, error) {
	return goals.get(ctx, s.db, id)
}

// ListGoals returns goals matching f, newest first.
func (s *Store) ListGoals(ctx context.Context, f store.GoalFilter) ([]domain.Goal, error) {
	var c cond
	if f.Project != "" {
		c.add("project = ?", f.Project)
	}
	if len(f.Statuses) > 0 {
		c.in("status", false, toAny(f.Statuses))
	}
	args := append(c.args, limitOr(f.Limit, 200))
	out, err := queryDocs[domain.Goal](ctx, s.db,
		"SELECT data FROM goals"+c.where()+" ORDER BY created_at DESC, id DESC LIMIT ?", args...)
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	return out, nil
}

// UpdateGoal applies fn to the goal with id in one transaction and returns the saved goal.
func (s *Store) UpdateGoal(ctx context.Context, id string, fn func(*domain.Goal) error) (domain.Goal, error) {
	return goals.update(ctx, s.db, id, fn)
}
