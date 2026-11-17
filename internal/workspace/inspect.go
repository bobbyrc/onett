package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotOnettCopy is wrapped by PrepareCopy when a directory already exists at the clone path and
// is not a clean clone of the project's repository: onett never takes over a person's checkout.
var ErrNotOnettCopy = errors.New("not a clean copy of the repository")

// PrepareCopy makes r.Path a fetched clone of r.Remote and reports whether it cloned it now.
// It checks out nothing (the caller calls EnsureClone once the default branch is known).
func (m *Manager) PrepareCopy(ctx context.Context, r Repo) (created bool, err error) {
	unlock := m.lockClone(r.Path)
	defer unlock()

	fi, err := os.Lstat(r.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(r.Path), 0o755); err != nil {
			return false, fmt.Errorf("create parent of clone %s: %w", r.Path, err)
		}
		// The clone does not exist yet, so no agent has written anything in it.
		if _, err := gitNet(ctx, "", "clone", remoteURL(r), r.Path); err != nil {
			return false, fmt.Errorf("clone %s: %w", r.Name, err)
		}
		// The clone exists now, so it is reported as created even when the fetch fails: the
		// caller must know to remove it.
		if err := m.fetchOrigin(ctx, r); err != nil {
			return true, fmt.Errorf("fetch %s: %w", r.Name, err)
		}
		return true, nil
	case err != nil:
		return false, fmt.Errorf("stat clone %s: %w", r.Path, err)
	case !fi.IsDir():
		return false, fmt.Errorf("%s is not a directory: %w", r.Path, ErrNotOnettCopy)
	}

	// A .git file is a linked worktree of some other clone, which is not a copy onett made.
	dot := filepath.Join(r.Path, ".git")
	if gi, err := os.Lstat(dot); err != nil || !gi.IsDir() {
		return false, fmt.Errorf("%s has no .git directory: %w", r.Path, ErrNotOnettCopy)
	}
	// The origin is read without running git inside the directory: such a git would rewrite the
	// clone's config.
	origin, err := configFileOrigin(ctx, filepath.Join(dot, "config"))
	if err != nil {
		return false, err
	}
	// The URL is left out of the error: it may hold a token.
	if origin == "" || originURL(r, origin) != origin {
		return false, fmt.Errorf("%s is a clone of another repository: %w", r.Path, ErrNotOnettCopy)
	}
	status, err := git(ctx, r.Path, "status", "--porcelain", "--ignore-submodules=all")
	if err != nil {
		return false, fmt.Errorf("status %s: %w", r.Name, err)
	}
	if status != "" {
		return false, fmt.Errorf("%s has uncommitted changes: %w", r.Path, ErrNotOnettCopy)
	}
	if err := m.fetchOrigin(ctx, r); err != nil {
		return false, fmt.Errorf("fetch %s: %w", r.Name, err)
	}
	return false, nil
}

// configFileOrigin returns remote.origin.url from the git config file at file, or "" when the
// file has no such key. It runs git outside the repository, with no credentials.
func configFileOrigin(ctx context.Context, file string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, hardening...),
		"config", "--file", file, "--get", "remote.origin.url")...)
	cmd.Dir = os.TempDir()
	cmd.Env = localEnv()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		// Exit status 1 is git's answer for a key that is not set.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("read origin of %s: %w", file, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// RemoteDefaultBranch returns the branch the remote's HEAD names, asked of the remote itself
// (git ls-remote --symref <url> HEAD in a scratch repository). r.Path must be a clone.
func (m *Manager) RemoteDefaultBranch(ctx context.Context, r Repo) (string, error) {
	unlock := m.lockClone(r.Path)
	defer unlock()

	url, err := syncOrigin(ctx, r)
	if err != nil {
		return "", err
	}
	s, err := newScratch(ctx, m.scratchDir(), r.Path)
	if err != nil {
		return "", err
	}
	defer s.close()
	out, err := s.gitNet(ctx, "ls-remote", "--symref", url, "HEAD")
	if err != nil {
		return "", err
	}
	// The line looks like "ref: refs/heads/master<TAB>HEAD".
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
			if branch, _, _ := strings.Cut(rest, "\t"); branch != "" {
				return branch, nil
			}
		}
	}
	return "", errors.New("the repository's HEAD names no branch")
}
