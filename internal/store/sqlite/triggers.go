package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

var triggers = entity[domain.Trigger]{
	name:  "trigger",
	table: "triggers",
	id:    func(t *domain.Trigger) *string { return &t.ID },
	stamp: func(t *domain.Trigger, now time.Time) { t.UpdatedAt = now },
	cols: func(t domain.Trigger) []col {
		return []col{
			{"id", t.ID},
			{"project", t.Project},
			{"created_at", nanos(t.CreatedAt)},
		}
	},
}

var triggerFirings = entity[domain.TriggerFiring]{
	name:  "trigger firing",
	table: "trigger_firings",
	id:    func(f *domain.TriggerFiring) *string { return &f.ID },
	stamp: nil,
	cols: func(f domain.TriggerFiring) []col {
		return []col{
			{"id", f.ID},
			{"trigger_id", f.TriggerID},
			{"issue_number", f.IssueNumber},
			{"at", nanos(f.At)},
		}
	},
}

// CreateTrigger stores t, assigning ID and timestamps when empty.
func (s *Store) CreateTrigger(ctx context.Context, t domain.Trigger) (domain.Trigger, error) {
	if t.ID == "" {
		t.ID = newID()
	}
	now := nowUTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	if err := triggers.insert(ctx, s.db, t); err != nil {
		return domain.Trigger{}, fmt.Errorf("create trigger: %w", err)
	}
	return t, nil
}

// GetTrigger returns the trigger with id, or ErrNotFound.
func (s *Store) GetTrigger(ctx context.Context, id string) (domain.Trigger, error) {
	return triggers.get(ctx, s.db, id)
}

// ListTriggers returns every trigger, oldest first.
func (s *Store) ListTriggers(ctx context.Context) ([]domain.Trigger, error) {
	out, err := queryDocs[domain.Trigger](ctx, s.db, "SELECT data FROM triggers ORDER BY created_at ASC, id ASC")
	if err != nil {
		return nil, fmt.Errorf("list triggers: %w", err)
	}
	return out, nil
}

// UpdateTrigger applies fn to the trigger with id in one transaction and returns the saved trigger.
func (s *Store) UpdateTrigger(ctx context.Context, id string, fn func(*domain.Trigger) error) (domain.Trigger, error) {
	return triggers.update(ctx, s.db, id, fn)
}

// DeleteTrigger removes the trigger and its firings in one transaction. ErrNotFound when absent.
func (s *Store) DeleteTrigger(ctx context.Context, id string) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM trigger_firings WHERE trigger_id = ?`, id); err != nil {
			return fmt.Errorf("delete trigger %s: %w", id, err)
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM triggers WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("delete trigger %s: %w", id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("delete trigger %s: %w", id, err)
		}
		if n == 0 {
			return fmt.Errorf("trigger %s: %w", id, store.ErrNotFound)
		}
		return nil
	})
}

// CreateTriggerFiring stores f, assigning ID and At when empty.
func (s *Store) CreateTriggerFiring(ctx context.Context, f domain.TriggerFiring) (domain.TriggerFiring, error) {
	if f.ID == "" {
		f.ID = newID()
	}
	if f.At.IsZero() {
		f.At = nowUTC()
	}
	if err := triggerFirings.insert(ctx, s.db, f); err != nil {
		return domain.TriggerFiring{}, fmt.Errorf("create trigger firing: %w", err)
	}
	return f, nil
}

// UpdateTriggerFiring applies fn to the firing with id in one transaction and returns the saved firing.
func (s *Store) UpdateTriggerFiring(ctx context.Context, id string, fn func(*domain.TriggerFiring) error) (domain.TriggerFiring, error) {
	return triggerFirings.update(ctx, s.db, id, fn)
}

// ListTriggerFirings returns the firings of one trigger, newest first. A limit of 0 means 200.
func (s *Store) ListTriggerFirings(ctx context.Context, triggerID string, limit int) ([]domain.TriggerFiring, error) {
	out, err := queryDocs[domain.TriggerFiring](ctx, s.db,
		"SELECT data FROM trigger_firings WHERE trigger_id = ? ORDER BY at DESC, id DESC LIMIT ?",
		triggerID, limitOr(limit, 200))
	if err != nil {
		return nil, fmt.Errorf("list trigger firings: %w", err)
	}
	return out, nil
}

// ListAllTriggerFirings returns the newest firings across every trigger, newest first. A limit of
// 0 means 50.
// shortcut: no index covers "at" alone, so this sorts the whole table; add one when firings are many.
func (s *Store) ListAllTriggerFirings(ctx context.Context, limit int) ([]domain.TriggerFiring, error) {
	out, err := queryDocs[domain.TriggerFiring](ctx, s.db,
		"SELECT data FROM trigger_firings ORDER BY at DESC, id DESC LIMIT ?", limitOr(limit, 50))
	if err != nil {
		return nil, fmt.Errorf("list all trigger firings: %w", err)
	}
	return out, nil
}

// TriggerFiringsSince returns the firings of one trigger at or after since, newest first.
func (s *Store) TriggerFiringsSince(ctx context.Context, triggerID string, since time.Time) ([]domain.TriggerFiring, error) {
	out, err := queryDocs[domain.TriggerFiring](ctx, s.db,
		"SELECT data FROM trigger_firings WHERE trigger_id = ? AND at >= ? ORDER BY at DESC, id DESC",
		triggerID, nanos(since))
	if err != nil {
		return nil, fmt.Errorf("trigger firings since: %w", err)
	}
	return out, nil
}

// LastIssueFiring returns the newest firing of triggerID for issueNumber, or ErrNotFound.
func (s *Store) LastIssueFiring(ctx context.Context, triggerID string, issueNumber int) (domain.TriggerFiring, error) {
	out, err := queryDocs[domain.TriggerFiring](ctx, s.db,
		"SELECT data FROM trigger_firings WHERE trigger_id = ? AND issue_number = ? ORDER BY at DESC, id DESC LIMIT 1",
		triggerID, issueNumber)
	if err != nil {
		return domain.TriggerFiring{}, fmt.Errorf("last issue firing: %w", err)
	}
	if len(out) == 0 {
		return domain.TriggerFiring{}, fmt.Errorf("%w: trigger firing for issue %d", store.ErrNotFound, issueNumber)
	}
	return out[0], nil
}
