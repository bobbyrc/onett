package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/notify"
	"github.com/bobbyrc/onett/internal/sysproc"
)

const (
	preflightTimeout = 5 * time.Second
	preflightPassTTL = time.Minute
	// A failure is rechecked sooner so runs resume soon after the owner fixes the cause.
	preflightFailTTL = 15 * time.Second
)

// preflight probes the directories an agent run reads from. On macOS a launchd service may be
// refused access to removable volumes: reads under such a path then block forever, while stat
// still works. onett doctor runs in the owner's Terminal, which has the access, so the check has
// to run inside the daemon.
type preflight struct {
	// readDir is the probe; tests replace it. timeout bounds one probe.
	readDir func(path string) error
	timeout time.Duration

	mu       sync.Mutex
	results  map[string]domain.Preflight
	stuck    map[string]chan struct{} // paths with a probe still blocked; closed when it returns
	notified map[string]bool          // projects whose current failure episode was announced
}

func newPreflight() *preflight {
	return &preflight{
		readDir:  func(path string) error { _, err := os.ReadDir(path); return err },
		timeout:  preflightTimeout,
		results:  map[string]domain.Preflight{},
		stuck:    map[string]chan struct{}{},
		notified: map[string]bool{},
	}
}

// preflightDirs lists the directories to probe for a project: each PATH entry an agent run gets
// (onett's environment plus the project's env), the project path and the worktrees directory.
func (e *Engine) preflightDirs(p *config.Project) []string {
	var dirs []string
	env := sysproc.AgentEnv(nil, p.Env)
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			for _, d := range filepath.SplitList(v) {
				if d != "" {
					dirs = append(dirs, d)
				}
			}
		}
	}
	dirs = append(dirs, p.Path)
	if e.d.Workspaces != nil && e.d.Workspaces.Root != "" {
		dirs = append(dirs, e.d.Workspaces.Root)
	}
	return dirs
}

// probe reads one directory with a timeout. A read blocked in the kernel cannot be cancelled, so
// on timeout its goroutine is leaked on purpose; the path stays in stuck so it is not probed
// again (and a second goroutine leaked) until that read returns.
func (pf *preflight) probe(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "" // a PATH entry that does not exist cannot hang
		}
		return err.Error()
	}
	pf.mu.Lock()
	if ch, ok := pf.stuck[resolved]; ok {
		select {
		case <-ch:
			delete(pf.stuck, resolved)
		default:
			pf.mu.Unlock()
			return "timed out (a previous read is still blocked)"
		}
	}
	done := make(chan struct{})
	res := make(chan error, 1)
	pf.stuck[resolved] = done
	pf.mu.Unlock()
	go func() {
		res <- pf.readDir(resolved)
		close(done)
	}()
	t := time.NewTimer(pf.timeout)
	defer t.Stop()
	select {
	case err := <-res:
		pf.mu.Lock()
		delete(pf.stuck, resolved)
		pf.mu.Unlock()
		if err != nil {
			return err.Error()
		}
		return ""
	case <-t.C:
		return fmt.Sprintf("read blocked for %s", pf.timeout)
	}
}

// checkPreflight returns the project's preflight, probing again when the cached result is stale.
// The first failure of an episode sends one high-priority notification; a pass ends the episode.
func (e *Engine) checkPreflight(ctx context.Context, p *config.Project) domain.Preflight {
	pf := e.pre
	now := e.d.Now()
	pf.mu.Lock()
	cur, ok := pf.results[p.Name]
	pf.mu.Unlock()
	ttl := preflightPassTTL
	if !cur.OK {
		ttl = preflightFailTTL
	}
	if ok && now.Sub(cur.CheckedAt) < ttl {
		return cur
	}

	res := domain.Preflight{Project: p.Name, OK: true, CheckedAt: now}
	for _, dir := range e.preflightDirs(p) {
		if msg := pf.probe(dir); msg != "" {
			res.OK, res.Path, res.Error = false, dir, msg
			res.Fix = preflightFixText(dir)
			break
		}
	}
	pf.mu.Lock()
	pf.results[p.Name] = res
	first := !res.OK && !pf.notified[p.Name]
	pf.notified[p.Name] = !res.OK
	pf.mu.Unlock()
	if first {
		msg := res.Summary() + ". No runs start for project " + p.Name + " until it is readable."
		e.notify(ctx, notify.PriorityHigh, "Cannot read a tool directory", msg, "/")
	}
	return res
}

// preflightFixText says what to do about an unreadable directory.
func preflightFixText(dir string) string {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		resolved = dir
	}
	if runtime.GOOS == "darwin" && strings.HasPrefix(resolved, "/Volumes/") {
		return "the onett service needs access to external volumes: System Settings > Privacy & Security > Full Disk Access (add the onett binary), or move this directory to the internal disk"
	}
	return "make sure the onett service can read it (permissions, or a disk that is not mounted)"
}

// resetPreflight forgets cached results, so the next scheduler pass probes every project again.
func (e *Engine) resetPreflight() {
	e.pre.mu.Lock()
	e.pre.results = map[string]domain.Preflight{}
	e.pre.mu.Unlock()
}

// Preflight implements api.Controller: the last result per project, sorted by project name.
func (e *Engine) Preflight() []domain.Preflight {
	e.pre.mu.Lock()
	defer e.pre.mu.Unlock()
	out := make([]domain.Preflight, 0, len(e.pre.results))
	for _, r := range e.pre.results {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b domain.Preflight) int { return strings.Compare(a.Project, b.Project) })
	return out
}
