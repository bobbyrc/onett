package workspace

import (
	"context"
	"fmt"
	"unicode/utf8"
)

// ReadFileAt returns the text of path as committed at ref in r's clone, and whether the file
// exists there. It reads git's object store, not a worktree, so it gives the committed text
// whatever a run has done to its own copy. It does not fetch: Prepare has fetched origin by the
// time a run asks. At most limit bytes are returned, cut on a rune boundary (no limit when limit
// is 0); a caller that must know whether the file was longer asks for one byte more than it
// keeps. A ref the clone does not have is an error, not a missing file; a path that is not a
// file at ref (absent, or a directory) is reported as missing.
// shortcut: the whole blob is read before it is cut, which is fine for an instruction file;
// stream it if this is ever used for arbitrary files.
func (m *Manager) ReadFileAt(ctx context.Context, r Repo, ref, path string, limit int) (string, bool, error) {
	unlock := m.lockClone(r.Path)
	defer unlock()
	if _, err := git(ctx, r.Path, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
		return "", false, fmt.Errorf("read %s at %s: no such commit: %w", path, ref, err)
	}
	object := ref + ":" + path
	kind, err := gitOutputAllowExit(ctx, r.Path, "cat-file", "-t", object)
	if err != nil {
		return "", false, err
	}
	if kind != "blob" {
		return "", false, nil
	}
	text, err := git(ctx, r.Path, "cat-file", "blob", object)
	if err != nil {
		return "", false, err
	}
	if limit > 0 && len(text) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut]
	}
	return text, true, nil
}
