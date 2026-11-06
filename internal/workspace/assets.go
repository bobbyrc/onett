package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// Limits on what PublishAssets takes from an agent-written directory.
const (
	maxAssetFiles = 48
	maxAssetBytes = 8 << 20
)

// assetLabel and assetName keep agent-chosen names to plain, URL-safe characters.
var (
	assetLabel = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	assetName  = regexp.MustCompile(`^[A-Za-z0-9._-]+\.png$`)
)

// AssetsBranch is the throwaway branch that holds a task branch's screenshots for one label.
// It is an orphan branch with no history in common with the code, and is never merged.
func AssetsBranch(branch, label string) string {
	return "pr-assets/" + branch + "/" + label
}

// PublishAssets commits the PNG files in srcDir to AssetsBranch(ws.Branch, label) on origin and
// returns the pushed commit and the file names, sorted. The agent wrote srcDir, so it is
// untrusted: only regular files (no symlinks) named like <safe-chars>.png, at most maxAssetFiles
// of at most maxAssetBytes each, are taken. The commit is made in a scratch repository, so the
// run's worktree and branch are untouched. It returns no files, and no error, when srcDir holds
// nothing usable.
func (m *Manager) PublishAssets(ctx context.Context, r Repo, ws Workspace, label, srcDir string) (string, []string, error) {
	if !assetLabel.MatchString(label) {
		return "", nil, fmt.Errorf("publish assets: bad label %q", label)
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, nil
		}
		return "", nil, err
	}
	var names []string
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxAssetBytes || !assetName.MatchString(e.Name()) {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) > maxAssetFiles {
		names = names[:maxAssetFiles]
	}
	if len(names) == 0 {
		return "", nil, nil
	}

	origin, err := git(ctx, ws.Dir, "remote", "get-url", "origin")
	if err != nil {
		return "", nil, err
	}
	tmp, err := os.MkdirTemp("", "onett-assets-")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(tmp)

	assets := AssetsBranch(ws.Branch, label)
	if _, err := git(ctx, tmp, "init", "--quiet"); err != nil {
		return "", nil, err
	}
	if _, err := git(ctx, tmp, "remote", "add", "origin", origin); err != nil {
		return "", nil, err
	}
	// Build on the remote assets branch when there is one, so earlier commits stay reachable.
	if _, err := git(ctx, tmp, "fetch", "--quiet", "origin", "refs/heads/"+assets); err == nil {
		_, err = git(ctx, tmp, "checkout", "--quiet", "-B", "assets", "FETCH_HEAD")
	} else {
		_, err = git(ctx, tmp, "checkout", "--quiet", "--orphan", "assets")
	}
	if err != nil {
		return "", nil, err
	}
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(srcDir, n))
		if err != nil {
			return "", nil, err
		}
		if err := os.WriteFile(filepath.Join(tmp, n), data, 0o644); err != nil {
			return "", nil, err
		}
	}
	if _, err := git(ctx, tmp, "add", "-A"); err != nil {
		return "", nil, err
	}
	if code, err := gitExitCode(ctx, tmp, "diff", "--cached", "--quiet"); err != nil {
		return "", nil, err
	} else if code != 0 {
		msg := "Screenshots: " + label + " for " + ws.Branch
		if _, err := git(ctx, tmp, "-c", "user.name=Onett", "-c", "user.email=onett@localhost",
			"commit", "--quiet", "-m", msg); err != nil {
			return "", nil, err
		}
	}
	sha, err := git(ctx, tmp, "rev-parse", "HEAD")
	if err != nil {
		return "", nil, err
	}
	if _, err := git(ctx, tmp, "push", "--quiet", "origin", "HEAD:refs/heads/"+assets); err != nil {
		return "", nil, fmt.Errorf("push %s: %w", assets, err)
	}
	return sha, names, nil
}
