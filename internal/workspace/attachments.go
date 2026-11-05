package workspace

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bobbyrc/onett/internal/attach"
	"github.com/bobbyrc/onett/internal/domain"
)

// AttachmentsDir is where a task's attachments are copied, relative to the worktree. It sits
// inside .onett-run/, which git ignores, so the files never reach a commit.
const AttachmentsDir = domain.OutboxDir + "/attachments"

// Placed is an attachment as it stands in a worktree. Expired means there was no file to copy
// (retention deleted it, or it could not be read); Rel and Abs are then empty.
type Placed struct {
	domain.Attachment
	Rel string // worktree-relative path
	Abs string // absolute path
}

// CopyAttachments copies each attachment's file into the worktree's attachments directory, mode
// 0600, under its sanitised name (prefixed with the attachment ID when the name is already taken).
// A file that is gone is reported as Expired rather than failing: the run goes ahead without it.
// It never reads or logs file contents.
func CopyAttachments(ws Workspace, atts []domain.Attachment) ([]Placed, error) {
	if len(atts) == 0 {
		return nil, nil
	}
	dir := filepath.Join(ws.Dir, filepath.FromSlash(AttachmentsDir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	used := map[string]bool{}
	out := make([]Placed, 0, len(atts))
	for _, a := range atts {
		p := Placed{Attachment: a}
		if a.Expired || a.Path == "" {
			p.Expired = true
			out = append(out, p)
			continue
		}
		name := attach.SanitizeName(a.Name)
		if used[name] {
			name = a.ID + "-" + name
		}
		dest := filepath.Join(dir, name)
		if rel, err := filepath.Rel(dir, dest); err != nil || rel != name || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("attachment %s: name %q leaves %s", a.ID, name, dir)
		}
		if err := copyFile(a.Path, dest); err != nil {
			var src *srcError
			if errors.As(err, &src) {
				p.Expired = true
				out = append(out, p)
				continue
			}
			return nil, fmt.Errorf("copy attachment %s: %w", a.ID, err)
		}
		used[name] = true
		p.Abs = dest
		p.Rel = filepath.ToSlash(filepath.Join(AttachmentsDir, name))
		out = append(out, p)
	}
	return out, nil
}

// srcError marks a failure to read the source file, as opposed to writing the destination.
type srcError struct{ err error }

func (e *srcError) Error() string { return e.err.Error() }
func (e *srcError) Unwrap() error { return e.err }

// copyFile copies the regular file src to a new file dst (mode 0600). A leftover dst is removed
// first and never followed, so a planted symlink cannot redirect the write.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return &srcError{err}
	}
	defer in.Close()
	if st, err := in.Stat(); err != nil || !st.Mode().IsRegular() {
		return &srcError{fmt.Errorf("not a regular file")}
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
