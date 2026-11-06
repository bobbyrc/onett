package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/workspace"
)

// githubSlug matches a bare "owner/name" repository, the only form with raw image URLs.
var githubSlug = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// shotsSection publishes the screenshots an agent left in <outbox>/shots/<label>/*.png and
// returns a Markdown section for the pull request description. Agents hold no push access, so
// onett pushes them (to a pr-assets branch) and links them by commit SHA. A failure only
// costs the section: it is logged and the run goes on.
func (e *Engine) shotsSection(ctx context.Context, t domain.Task, p *config.Project, ws workspace.Workspace) string {
	if !githubSlug.MatchString(p.Repo) {
		return "" // no raw image URLs for a local or non-GitHub remote
	}
	root := filepath.Join(ws.Dir, domain.OutboxDir, domain.ShotsDir)
	dirs, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var labels []string
	for _, d := range dirs {
		if d.IsDir() {
			labels = append(labels, d.Name())
		}
	}
	// "before" reads first, then the rest alphabetically ("after" included).
	sort.Slice(labels, func(i, j int) bool {
		if (labels[i] == "before") != (labels[j] == "before") {
			return labels[i] == "before"
		}
		return labels[i] < labels[j]
	})
	var out strings.Builder
	for _, label := range labels {
		sha, names, err := e.d.Workspaces.PublishAssets(ctx, e.repoFor(p), ws, label, filepath.Join(root, label))
		if err != nil {
			e.d.Logger.Warn("publish screenshots", "task", t.ID, "label", label, "err", err)
			continue
		}
		if len(names) == 0 {
			continue
		}
		base := "https://raw.githubusercontent.com/" + p.Repo + "/" + sha + "/"
		fmt.Fprintf(&out, "\n\n### Screenshots: %s\n\n", label)
		for _, n := range names {
			fmt.Fprintf(&out, "![%s](%s%s)\n", strings.TrimSuffix(n, ".png"), base, n)
		}
	}
	return out.String()
}
