package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

const attachmentCols = `id, conversation_id, name, mime, size, path, created_at`

func scanAttachment(r rowScanner) (domain.Attachment, error) {
	var a domain.Attachment
	var created int64
	if err := r.Scan(&a.ID, &a.ConversationID, &a.Name, &a.MIME, &a.Size, &a.Path, &created); err != nil {
		return domain.Attachment{}, err
	}
	a.CreatedAt = fromNanos(created)
	return a, nil
}

// CreateAttachment stores the record of a file already on disk.
func (s *Store) CreateAttachment(ctx context.Context, a domain.Attachment) (domain.Attachment, error) {
	if a.ID == "" {
		a.ID = newID()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = nowUTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO attachments (`+attachmentCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.ConversationID, a.Name, a.MIME, a.Size, a.Path, nanos(a.CreatedAt))
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("create attachment: %w", err)
	}
	return a, nil
}

// GetAttachment returns the attachment with id, or ErrNotFound.
func (s *Store) GetAttachment(ctx context.Context, id string) (domain.Attachment, error) {
	a, err := scanAttachment(s.db.QueryRowContext(ctx,
		`SELECT `+attachmentCols+` FROM attachments WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Attachment{}, fmt.Errorf("attachment %s: %w", id, store.ErrNotFound)
	}
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("get attachment %s: %w", id, err)
	}
	return a, nil
}

// ListAttachmentsBefore returns the attachments created before the cutoff, oldest first.
func (s *Store) ListAttachmentsBefore(ctx context.Context, before time.Time) ([]domain.Attachment, error) {
	out, err := queryRows(ctx, s.db, scanAttachment,
		`SELECT `+attachmentCols+` FROM attachments WHERE created_at < ? ORDER BY created_at ASC, id ASC`,
		nanos(before))
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	return out, nil
}

// DeleteAttachment removes the record and every message's reference to it.
func (s *Store) DeleteAttachment(ctx context.Context, id string) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM chat_message_attachments WHERE attachment_id = ?`, id); err != nil {
			return fmt.Errorf("delete attachment links %s: %w", id, err)
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM attachments WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("delete attachment %s: %w", id, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("attachment %s: %w", id, store.ErrNotFound)
		}
		return nil
	})
}

// linkAttachments records that message messageID carries the attachments ids, in order. Each
// must exist and belong to conversationID, or the whole append fails.
func linkAttachments(ctx context.Context, tx *sql.Tx, messageID, conversationID string, ids []string) error {
	for i, id := range ids {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO chat_message_attachments (message_id, attachment_id, position)
			 SELECT ?, id, ? FROM attachments WHERE id = ? AND conversation_id = ?`,
			messageID, i, id, conversationID)
		if err != nil {
			return fmt.Errorf("link attachment %s: %w", id, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("attachment %s: %w", id, store.ErrNotFound)
		}
	}
	return nil
}

// hydrateAttachments fills Attachments on msgs from the links table. where selects the link rows
// (a condition on l.message_id). Path is loaded too (it is never serialised) so a queued message
// can be turned into a prompt later. A message whose attachment was deleted simply has fewer.
func hydrateAttachments(ctx context.Context, ex execer, msgs []domain.ChatMessage, where string, args ...any) error {
	if len(msgs) == 0 {
		return nil
	}
	type link struct {
		message string
		att     domain.Attachment
	}
	links, err := queryRows(ctx, ex, func(r rowScanner) (link, error) {
		var l link
		var created int64
		err := r.Scan(&l.message, &l.att.ID, &l.att.ConversationID, &l.att.Name, &l.att.MIME, &l.att.Size, &l.att.Path, &created)
		l.att.CreatedAt = fromNanos(created)
		return l, err
	}, `SELECT l.message_id, a.id, a.conversation_id, a.name, a.mime, a.size, a.path, a.created_at
		FROM chat_message_attachments l JOIN attachments a ON a.id = l.attachment_id
		WHERE `+where+` ORDER BY l.position ASC`, args...)
	if err != nil {
		return fmt.Errorf("load message attachments: %w", err)
	}
	byMsg := map[string][]domain.Attachment{}
	for _, l := range links {
		byMsg[l.message] = append(byMsg[l.message], l.att)
	}
	for i := range msgs {
		msgs[i].Attachments = byMsg[msgs[i].ID]
	}
	return nil
}

// linkTaskAttachments records the attachments ids as the task's, in order. Each must exist; the
// name, type and size are copied into the link so they survive retention deleting the file.
func linkTaskAttachments(ctx context.Context, ex execer, taskID string, ids []string) error {
	for i, id := range ids {
		res, err := ex.ExecContext(ctx,
			`INSERT INTO task_attachments (task_id, attachment_id, position, conversation_id, name, mime, size)
			 SELECT ?, id, ?, conversation_id, name, mime, size FROM attachments WHERE id = ?`,
			taskID, i, id)
		if err != nil {
			return fmt.Errorf("link attachment %s: %w", id, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("attachment %s: %w", id, store.ErrNotFound)
		}
	}
	return nil
}

// loadTaskAttachments returns the task's attachments in order. Path is loaded (it is never
// serialised); an attachment whose record retention has deleted comes back Expired, with no Path.
func loadTaskAttachments(ctx context.Context, ex execer, taskID string) ([]domain.Attachment, error) {
	out, err := queryRows(ctx, ex, func(r rowScanner) (domain.Attachment, error) {
		var a domain.Attachment
		var created int64
		var live int
		err := r.Scan(&a.ID, &a.ConversationID, &a.Name, &a.MIME, &a.Size, &a.Path, &created, &live)
		a.CreatedAt = fromNanos(created)
		a.Expired = live == 0
		return a, err
	}, `SELECT l.attachment_id, l.conversation_id, l.name, l.mime, l.size,
			COALESCE(a.path, ''), COALESCE(a.created_at, 0), a.id IS NOT NULL
		FROM task_attachments l LEFT JOIN attachments a ON a.id = l.attachment_id
		WHERE l.task_id = ? ORDER BY l.position ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("load task attachments: %w", err)
	}
	return out, nil
}
