package workspace

import (
	"context"
	"fmt"
	"strings"
)

// ReadFileAt returns the text of the file at path in the commit that ref names, in r's clone, with
// surrounding whitespace trimmed, and whether there is such a file. It reads git's object store,
// not a worktree, so it gives the committed text whatever a run has done to its own copy. Give
// ref in full ("refs/remotes/origin/main"): a short name can be shadowed by a tag or a branch of
// the same name, which any worktree of the clone can create. Replace refs are ignored for the
// same reason. It does not fetch: Prepare has fetched origin by the time a run asks. A path that
// is absent at ref, or is a directory, a submodule or a symbolic link (links are not followed),
// is reported as missing. A ref the clone does not have, or a git failure, is an error.
// shortcut: the whole blob is read into memory, which is fine for an instruction file; stream it
// if this is ever used for arbitrary files.
func (m *Manager) ReadFileAt(ctx context.Context, r Repo, ref, path string) (string, bool, error) {
	unlock := m.lockClone(r.Path)
	defer unlock()
	// One line for the entry, "<mode> <type> <object>\t<path>", or nothing when ref has no such path.
	entry, err := git(ctx, r.Path, "--no-replace-objects", "ls-tree", ref, "--", path)
	if err != nil {
		return "", false, fmt.Errorf("read %s at %s: %w", path, ref, err)
	}
	meta, _, _ := strings.Cut(entry, "\t")
	fields := strings.Fields(meta)
	// Modes 100644 and 100755 are regular files; 120000 is a symbolic link.
	if len(fields) != 3 || fields[1] != "blob" || !strings.HasPrefix(fields[0], "100") {
		return "", false, nil
	}
	text, err := git(ctx, r.Path, "--no-replace-objects", "cat-file", "blob", fields[2])
	if err != nil {
		return "", false, fmt.Errorf("read %s at %s: %w", path, ref, err)
	}
	return text, true, nil
}
