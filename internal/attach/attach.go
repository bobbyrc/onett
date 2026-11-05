// Package attach stores the files the owner attaches to chat: photos, PDFs, logs. Files live under
// <data_dir>/attachments/<conversation id>/<attachment id>-<name> and their records in the store.
// It knows nothing of HTTP, so the API and the chat channels save through the same Save.
//
// Nothing here executes or interprets a file. The name is cleaned before it touches the disk, the
// type is sniffed from the bytes, and the size limit is enforced while streaming.
package attach

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

var (
	// ErrTooLarge means the upload went over the size limit; nothing was kept.
	ErrTooLarge = errors.New("attachment is too large")
	// ErrInvalid means the request names attachments that cannot be used.
	ErrInvalid = errors.New("invalid attachment")
)

// maxNameLen caps a stored file name, in bytes.
const maxNameLen = 100

// Manager saves, reads and expires attachments.
type Manager struct {
	store store.Store
	dir   string
	// maxSize is asked on every Save so a config reload takes effect at once.
	maxSize func() int64
	now     func() time.Time
}

// New returns a Manager keeping files under dir (created on first use). maxSize reports the
// current per-file limit in bytes.
func New(st store.Store, dir string, maxSize func() int64) *Manager {
	return &Manager{store: st, dir: dir, maxSize: maxSize, now: time.Now}
}

// Dir is the directory all attachments live under.
func (m *Manager) Dir() string { return m.dir }

// MaxSize is the current per-file limit in bytes.
func (m *Manager) MaxSize() int64 { return m.maxSize() }

// Save stores r as a new attachment of the conversation. The name is only a label: its base name
// is cleaned and used in the file name. It returns ErrTooLarge, with no file left behind, when r
// holds more than the limit.
func (m *Manager) Save(ctx context.Context, conversationID, name string, r io.Reader) (domain.Attachment, error) {
	if conversationID == "" || strings.ContainsAny(conversationID, `/\`) || conversationID == "." || conversationID == ".." {
		return domain.Attachment{}, fmt.Errorf("%w: bad conversation id", ErrInvalid)
	}
	limit := m.maxSize()
	id := uuid.Must(uuid.NewV7()).String()
	clean := SanitizeName(name)
	convDir := filepath.Join(m.dir, conversationID)
	path := filepath.Join(convDir, id+"-"+clean)
	if err := m.inside(path); err != nil {
		return domain.Attachment{}, err
	}
	if err := os.MkdirAll(convDir, 0o700); err != nil {
		return domain.Attachment{}, fmt.Errorf("create attachment dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("create attachment file: %w", err)
	}
	keep := false
	defer func() {
		_ = f.Close()
		if !keep {
			_ = os.Remove(path)
			_ = os.Remove(convDir) // only succeeds when empty
		}
	}()

	// Read one byte past the limit to tell "exactly at the limit" from "over it".
	head := make([]byte, 512)
	n, err := io.ReadFull(io.LimitReader(r, limit+1), head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return domain.Attachment{}, readErr(err)
	}
	head = head[:n]
	size := int64(n)
	if size > limit {
		return domain.Attachment{}, ErrTooLarge
	}
	if _, err := f.Write(head); err != nil {
		return domain.Attachment{}, fmt.Errorf("write attachment: %w", err)
	}
	if n == 512 {
		rest, err := io.Copy(f, io.LimitReader(r, limit+1-size))
		if err != nil {
			return domain.Attachment{}, readErr(err)
		}
		size += rest
		if size > limit {
			return domain.Attachment{}, ErrTooLarge
		}
	}
	if size == 0 {
		return domain.Attachment{}, fmt.Errorf("%w: the file is empty", ErrInvalid)
	}
	if err := f.Close(); err != nil {
		return domain.Attachment{}, fmt.Errorf("write attachment: %w", err)
	}

	a, err := m.store.CreateAttachment(ctx, domain.Attachment{
		ID: id, ConversationID: conversationID, Name: clean, MIME: Sniff(head), Size: size,
		CreatedAt: m.now().UTC(), Path: path,
	})
	if err != nil {
		return domain.Attachment{}, err
	}
	keep = true
	return a, nil
}

// readErr passes through the error from an upload body, keeping a body-too-large error
// recognisable as ErrTooLarge (the HTTP layer caps the whole request as well).
func readErr(err error) error {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return ErrTooLarge
	}
	return fmt.Errorf("read upload: %w", err)
}

// Get returns the attachment's record.
func (m *Manager) Get(ctx context.Context, id string) (domain.Attachment, error) {
	return m.store.GetAttachment(ctx, id)
}

// Open returns the record and the open file. A file already deleted from disk is ErrNotFound.
// The caller closes the file.
func (m *Manager) Open(ctx context.Context, id string) (domain.Attachment, *os.File, error) {
	a, err := m.store.GetAttachment(ctx, id)
	if err != nil {
		return domain.Attachment{}, nil, err
	}
	if err := m.inside(a.Path); err != nil {
		return domain.Attachment{}, nil, err
	}
	f, err := os.Open(a.Path)
	if errors.Is(err, os.ErrNotExist) {
		return domain.Attachment{}, nil, fmt.Errorf("attachment %s: %w", id, store.ErrNotFound)
	}
	if err != nil {
		return domain.Attachment{}, nil, fmt.Errorf("open attachment %s: %w", id, err)
	}
	return a, f, nil
}

// Validate checks the attachments a message wants to carry: at most max of them, no repeats, each
// existing and belonging to the conversation. It returns the records in order. Errors wrap
// ErrInvalid.
func (m *Manager) Validate(ctx context.Context, conversationID string, ids []string, max int) ([]domain.Attachment, error) {
	if len(ids) > max {
		return nil, fmt.Errorf("%w: at most %d attachments per message", ErrInvalid, max)
	}
	seen := map[string]bool{}
	out := make([]domain.Attachment, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			return nil, fmt.Errorf("%w: attachment %s is listed twice", ErrInvalid, id)
		}
		seen[id] = true
		a, err := m.store.GetAttachment(ctx, id)
		if errors.Is(err, store.ErrNotFound) || (err == nil && a.ConversationID != conversationID) {
			return nil, fmt.Errorf("%w: unknown attachment %s", ErrInvalid, id)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// Prune deletes every attachment created before the cutoff: the file, the record, and the
// conversation's directory once empty. It returns how many it deleted. One that fails is skipped
// (and reported in the returned error) so it does not hold back the rest.
func (m *Manager) Prune(ctx context.Context, before time.Time) (int, error) {
	old, err := m.store.ListAttachmentsBefore(ctx, before)
	if err != nil {
		return 0, err
	}
	var deleted int
	var errs []error
	for _, a := range old {
		if err := m.inside(a.Path); err != nil {
			errs = append(errs, fmt.Errorf("attachment %s: %w", a.ID, err))
			continue
		}
		if err := os.Remove(a.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove attachment %s: %w", a.ID, err))
			continue
		}
		if err := m.store.DeleteAttachment(ctx, a.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
			errs = append(errs, err)
			continue
		}
		_ = os.Remove(filepath.Dir(a.Path)) // only succeeds when the directory is empty
		deleted++
	}
	return deleted, errors.Join(errs...)
}

// inside reports an error unless path is strictly below the attachments directory.
func (m *Manager) inside(path string) error {
	rel, err := filepath.Rel(m.dir, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("%w: path escapes the attachments directory", ErrInvalid)
	}
	return nil
}

// SanitizeName reduces a client-supplied file name to a safe label: the base name only (either
// kind of slash splits), no control characters, no leading dots, at most maxNameLen bytes, and
// "file" when nothing is left.
func SanitizeName(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '�' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return -1
		}
		return r
	}, name)
	name = strings.ReplaceAll(name, "..", "")
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	name = strings.TrimSpace(name)
	if len(name) > maxNameLen {
		ext := filepath.Ext(name)
		if len(ext) > 16 {
			ext = ""
		}
		cut := maxNameLen - len(ext)
		for cut > 0 && !isRuneStart(name[cut]) {
			cut--
		}
		name = name[:cut] + ext
	}
	if name == "" {
		return "file"
	}
	return name
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// Sniff reports the media type of content from its first bytes. The client's Content-Type and
// the file extension are never consulted. The result has no parameters ("text/plain").
func Sniff(head []byte) string {
	// HEIC/HEIF photos (iPhones) are ISO base media files that DetectContentType does not know.
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		switch string(head[8:12]) {
		case "heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1":
			return "image/heic"
		}
	}
	t, _, err := mime.ParseMediaType(http.DetectContentType(head))
	if err != nil {
		return "application/octet-stream"
	}
	return t
}
