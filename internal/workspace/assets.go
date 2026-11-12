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
// run's worktree and branch are untouched; the scratch repository is the only place onett's
// credentials are used. It returns no files, and no error, when srcDir holds
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

	url, err := syncOrigin(ctx, r)
	if err != nil {
		return "", nil, err
	}
	s, err := newScratch(ctx, m.scratchDir(), r.Path)
	if err != nil {
		return "", nil, err
	}
	defer s.close()
	tmp := s.dir

	assets := AssetsBranch(ws.Branch, label)
	// Build on the remote assets branch when there is one, so earlier commits stay reachable.
	if _, err := s.gitNet(ctx, "fetch", "--quiet", url, "refs/heads/"+assets); err == nil {
		_, err = s.git(ctx, "checkout", "--quiet", "-B", "assets", "FETCH_HEAD")
	} else {
		_, err = s.git(ctx, "checkout", "--quiet", "--orphan", "assets")
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
	if _, err := s.git(ctx, "add", "-A"); err != nil {
		return "", nil, err
	}
	if code, err := gitExitCode(ctx, tmp, "diff", "--cached", "--quiet"); err != nil {
		return "", nil, err
	} else if code != 0 {
		msg := "Screenshots: " + label + " for " + ws.Branch
		if _, err := s.git(ctx, "-c", "user.name=Onett", "-c", "user.email=onett@localhost",
			"commit", "--quiet", "-m", msg); err != nil {
			return "", nil, err
		}
	}
	sha, err := s.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", nil, err
	}
	if _, err := s.gitNet(ctx, "push", "--quiet", url, "HEAD:refs/heads/"+assets); err != nil {
		return "", nil, fmt.Errorf("push %s: %w", assets, err)
	}
	return sha, names, nil
}
