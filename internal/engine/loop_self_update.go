package engine

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/notify"
	"github.com/bobbyrc/onett/internal/workspace"
)

// selfUpdateMaxFailures is how many times an update to one commit may fail before the loop stops
// trying that commit. The count is in memory and resets when the daemon restarts.
const selfUpdateMaxFailures = 3

// selfUpdateTailLines and selfUpdateTailBytes bound the log excerpt sent with a failure notice.
const (
	selfUpdateTailLines = 15
	selfUpdateTailBytes = 1500
)

// Files in data_dir that tie the daemon and its updater together.
const (
	selfUpdateLogFile    = "self-update.log"
	selfUpdateLockFile   = "self-update.lock"   // the updater's PID
	selfUpdateResultFile = "self-update.result" // old, new, ref and status, written by update.sh
)

// selfUpdateFailureState is the LoopState name that keeps a project's last failed self-update (a
// selfUpdateFailure as JSON in Cursor). The heartbeat lists it from here, not from its event,
// because an event ages out while the daemon may still be running the version that failed to
// update.
const selfUpdateFailureState = "self_update_failure"

// selfUpdateFailure is the last update that failed: it could not start, exited with an error, or
// was rolled back.
type selfUpdateFailure struct {
	Ref     string    `json:"ref"`
	Version string    `json:"version"` // the version the daemon ran when the update failed
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// recordSelfUpdateFailure stores the failure, redacted, for the heartbeat. Each call replaces the whole row,
// so the loop and the goroutine that waits for the updater cannot lose each other's write.
func (e *Engine) recordSelfUpdateFailure(ctx context.Context, project, ref, message string) {
	// The message can quote the updater's exit error or a rollback reason from its result file, and
	// the heartbeat shows it to an agent, so it is redacted before it is stored. The updater runs
	// with onett's own environment, so that is what could leak.
	message = redactSecrets(message, os.Environ())
	raw, err := json.Marshal(selfUpdateFailure{Ref: ref, Version: e.d.Version, Message: message, At: e.d.Now().UTC()})
	if err == nil {
		err = e.d.Store.SaveLoopState(ctx, domain.LoopState{Name: selfUpdateFailureState, Project: project, Cursor: string(raw)})
	}
	if err != nil {
		e.d.Logger.Error("self_update: record failure", "err", err)
	}
}

// openSelfUpdateFailure returns the project's last failed self-update while it is still open:
// while the daemon runs the version that failed to update. Once it runs any other version, an
// update has succeeded since (by this loop or by hand), so the failure is history.
func (e *Engine) openSelfUpdateFailure(ctx context.Context, project string) (selfUpdateFailure, bool, error) {
	st, err := e.d.Store.GetLoopState(ctx, selfUpdateFailureState, project)
	if err != nil {
		return selfUpdateFailure{}, false, err
	}
	var f selfUpdateFailure
	if st.Cursor == "" || json.Unmarshal([]byte(st.Cursor), &f) != nil || f.Ref == "" || f.Version != e.d.Version {
		return selfUpdateFailure{}, false, nil
	}
	return f, true, nil
}

// selfUpdateLog appends one timestamped line to self-update.log at path, so a reader can tell when
// each attempt started and how it ended (#244). The updater's own lines in the same file are not
// stamped: the updater must outlive the daemon it restarts, so its output goes straight to the
// file, and stamping each line would put a pipe under it, whose reader dying would kill the
// updater mid-update.
func (e *Engine) selfUpdateLog(path, text string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		e.d.Logger.Warn("self_update: write log", "err", err)
		return
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%s onett: %s\n", e.d.Now().UTC().Format(time.RFC3339), text); err != nil {
		e.d.Logger.Warn("self_update: write log", "err", err)
	}
}

// selfUpdateState is what the self_update loop remembers between ticks.
type selfUpdateState struct {
	mu    sync.Mutex
	fails map[string]int // failed attempts by full commit SHA
	// fetched is the last release the clone was fetched for, so that one release costs one
	// successful fetch.
	fetched string
	// isAncestor replaces Workspaces.IsAncestor in tests whose releases are not real commits.
	// Nil, as in production, means git.
	isAncestor func(ctx context.Context, r workspace.Repo, ancestor, descendant string) (bool, error)
}

func (s *selfUpdateState) failures(ref string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fails[ref]
}

func (s *selfUpdateState) fail(ref string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fails == nil {
		s.fails = map[string]int{}
	}
	s.fails[ref] += n
}

// fetchedFor reports whether the clone has already been fetched for release ref.
func (s *selfUpdateState) fetchedFor(ref string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fetched == ref
}

// markFetched remembers that the clone was fetched for release ref.
func (s *selfUpdateState) markFetched(ref string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fetched = ref
}

// selfUpdateLoop updates the running daemon to the self project's latest release once that
// release has been healthy on staging for min_healthy and is ahead of the commit the daemon runs.
// Code decides when; the configured command does the work (for the dogfood setup,
// examples/dogfood/update.sh). The command is started detached and not waited for: it restarts
// this daemon, so it has to outlive it.
func (e *Engine) selfUpdateLoop(ctx context.Context, p *config.Project) error {
	cfg := e.Config()
	su := cfg.LoopsFor(p.Name).SelfUpdate
	if su.Command == "" {
		return errors.New("self_update: no command configured")
	}
	dep, ok, hold, err := e.selfUpdateEligible(ctx, p)
	if err != nil {
		return err
	}
	e.selfUpdateHold(ctx, p.Name, hold)
	if !ok {
		return nil
	}
	version := e.d.Version

	// The updater restarts the daemon. Agent runs, chat turns and goal turns carry on across a
	// restart, but a release's deploy command is a child of this daemon and would be killed by it,
	// so wait until no project has a release in flight. Nothing is paused for this: a new release
	// starts only on a tick when none is in flight, and the next tick after it settles gets here.
	inFlight, err := e.releaseInFlight(ctx)
	if err != nil {
		return fmt.Errorf("self_update: %w", err)
	}
	if inFlight {
		e.d.Logger.Info("self_update: update held back", "ref", dep.Ref, "reason", "a release is in flight")
		return nil
	}

	dataDir := cfg.Server.DataDir
	lock := filepath.Join(dataDir, selfUpdateLockFile)
	if updaterRunning(lock) {
		e.d.Logger.Info("self_update: update held back", "ref", dep.Ref, "reason", "an updater is already running")
		return nil
	}

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("self_update: %w", err)
	}
	logPath := filepath.Join(dataDir, selfUpdateLogFile)
	e.selfUpdateLog(logPath, "starting update to "+dep.Ref+" (running "+version+")")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("self_update: open log: %w", err)
	}
	defer logFile.Close() // the child holds its own copy of the descriptor

	cmd := updaterCommand(su.Command, dep.Ref, dataDir, logFile)
	start := e.d.StartUpdater
	if start == nil {
		start = startDetached
	}
	pid, waitCmd, err := start(cmd)
	if err != nil {
		e.selfUpdateFailed(ctx, p.Name, dep.Ref, logPath, "could not start: "+err.Error())
		return nil
	}
	// A failed lock write is not fatal: the scheduler runs one tick of a loop at a time.
	if err := os.WriteFile(lock, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		e.d.Logger.Warn("self_update: write lock", "err", err)
	}

	short := shortSHA(dep.Ref)
	msg := "Updating onett to " + short
	e.d.Bus.Emit(ctx, domain.EvSystem, p.Name, "", "", msg, map[string]string{"ref": dep.Ref, "from": version})
	e.notify(ctx, notify.PriorityNormal, msg, "Release "+short+" has been healthy on staging; running "+version+".", "")

	// Not goTracked: Run waits for tracked goroutines on shutdown, and the updater is what shuts
	// this daemon down.
	bg := context.WithoutCancel(ctx)
	go func() {
		if werr := waitCmd(); werr != nil {
			e.selfUpdateFailed(bg, p.Name, dep.Ref, logPath, "exited with an error: "+werr.Error())
		}
	}()
	return nil
}

// selfUpdateHold keeps why the loop cannot tell whether a release is newer than the running build
// ("" when it can) in the loop's own state row, and says a new reason once, in the daemon's log
// and in self-update.log, not on every pass. It is not the loop's error: nothing is failing that
// a retry would fix, so the owner is not alerted.
func (e *Engine) selfUpdateHold(ctx context.Context, project, why string) {
	// The reason can quote a git error, so it is redacted before it is stored or logged.
	why = redactSecrets(why, os.Environ())
	st, err := e.d.Store.GetLoopState(ctx, loopSelfUpdate, project)
	if err != nil {
		e.d.Logger.Error("self_update: read loop state", "err", err)
		return
	}
	if st.Cursor == why {
		return
	}
	st.Cursor = why
	if err := e.d.Store.SaveLoopState(ctx, st); err != nil {
		e.d.Logger.Error("self_update: save loop state", "err", err)
		return
	}
	if why != "" {
		e.d.Logger.Warn("self_update: not updating", "reason", why)
		e.selfUpdateLog(filepath.Join(e.Config().Server.DataDir, selfUpdateLogFile), "not updating: "+why)
	}
}

// selfUpdateScan is how many recent deployments selfUpdateTarget looks through.
const selfUpdateScan = 50

// selfUpdateTarget picks the release to update to: the newest one that is healthy, has been for
// min_healthy, has not failed too often, and is ahead of the commit the daemon was built from.
// Newer releases do not hide it, because with merges landing every few minutes the newest one is
// rarely old enough. The scan stops at the release the daemon already runs, and at the first
// candidate that is not ahead of the running build: a daemon updated by hand can run a commit
// newer than every release, and moving it to an older release is not an update. hold says why,
// when git cannot compare the running build with a candidate; then nothing is chosen.
func (e *Engine) selfUpdateTarget(ctx context.Context, p *config.Project, minHealthy time.Duration, version string) (dep domain.Deployment, ok bool, hold string, err error) {
	deps, err := e.d.Store.ListDeployments(ctx, p.Name, selfUpdateScan) // newest first
	if err != nil {
		return domain.Deployment{}, false, "", fmt.Errorf("self_update: list deployments: %w", err)
	}
	now := e.d.Now()
	rev := versionRev(version)
	for _, d := range deps {
		if versionHasRef(version, d.Ref) {
			break
		}
		if d.State != domain.DeployHealthy || now.Sub(d.UpdatedAt) < minHealthy {
			continue
		}
		if e.selfUpd.failures(d.Ref) >= selfUpdateMaxFailures {
			continue
		}
		ahead, err := e.selfUpdateAhead(ctx, p, rev, d.Ref)
		if err != nil && !e.selfUpd.fetchedFor(d.Ref) {
			// Nothing fetches the clone when a release merges, so its commit may not be there yet.
			// A fetch that fails is tried again on the next pass. One that worked is not repeated
			// for this release: a commit that is still unknown after it stays unknown.
			if ferr := e.d.Workspaces.Fetch(ctx, e.repoFor(p)); ferr == nil {
				e.selfUpd.markFetched(d.Ref)
				ahead, err = e.selfUpdateAhead(ctx, p, rev, d.Ref)
			}
		}
		if err != nil {
			return domain.Deployment{}, false, fmt.Sprintf("cannot tell whether release %s is newer than the running build (version %q): %v", shortSHA(d.Ref), version, err), nil
		}
		if !ahead {
			break
		}
		return d, true, "", nil
	}
	return domain.Deployment{}, false, "", nil
}

// selfUpdateAhead reports whether release commit ref is a strict descendant of rev, the commit
// the daemon was built from, in the self project's clone. A commit the clone does not have is an
// error, like any other git failure: the caller must not guess.
func (e *Engine) selfUpdateAhead(ctx context.Context, p *config.Project, rev, ref string) (bool, error) {
	isAncestor := e.selfUpd.isAncestor
	if isAncestor == nil {
		isAncestor = e.d.Workspaces.IsAncestor
	}
	repo := e.repoFor(p)
	// Asked both ways, because IsAncestor is also true for the same commit, and a version that
	// names that commit another way (a tag) would otherwise be "updated" to itself on every pass.
	behind, err := isAncestor(ctx, repo, ref, rev)
	if err != nil || behind {
		return false, err
	}
	return isAncestor(ctx, repo, rev, ref)
}

// selfUpdateEligible returns the release the daemon should update to now, if any. Both the
// self_update loop and the deploy loop's hold-back use it, so they cannot disagree. hold says why
// when the loop cannot tell whether any release is newer than the running build: a dev build or
// one with uncommitted changes names no commit to compare, and selfUpdateTarget reports a commit
// git cannot place. A paused project does not update.
func (e *Engine) selfUpdateEligible(ctx context.Context, p *config.Project) (dep domain.Deployment, ok bool, hold string, err error) {
	su := e.Config().LoopsFor(p.Name).SelfUpdate
	version := e.d.Version
	switch {
	case version == "" || strings.HasPrefix(version, "dev"):
		return domain.Deployment{}, false, fmt.Sprintf("the running build (version %q) is a dev build, which never replaces itself", version), nil
	case strings.HasSuffix(version, "-dirty"):
		return domain.Deployment{}, false, fmt.Sprintf("the running build (version %q) has uncommitted changes, so no release is known to be newer", version), nil
	case e.projectPaused(p.Name):
		return domain.Deployment{}, false, "", nil
	}
	return e.selfUpdateTarget(ctx, p, su.MinHealthy.Duration, version)
}

// selfUpdateWaiting reports whether a self-update target is eligible, so the deploy loop should
// not start another release: a new release in flight would hold the updater back again.
func (e *Engine) selfUpdateWaiting(ctx context.Context) bool {
	cfg := e.Config()
	name := cfg.SelfProject
	p := cfg.Projects[name]
	if name == "" || p == nil || !loopOn(cfg, loopSelfUpdate, name) || cfg.LoopsFor(name).SelfUpdate.Command == "" {
		return false
	}
	_, ok, _, err := e.selfUpdateEligible(ctx, p)
	return err == nil && ok
}

// releaseInFlight reports whether any project has a release being deployed or verified. A
// release's deploy command runs as a child of this daemon, so a restart would cut it off.
func (e *Engine) releaseInFlight(ctx context.Context) (bool, error) {
	deps, err := e.d.Store.ListDeployments(ctx, "", 50)
	if err != nil {
		return false, fmt.Errorf("list deployments: %w", err)
	}
	for _, d := range deps {
		switch d.State {
		case domain.DeployPending, domain.DeployDeploying, domain.DeployVerifying:
			return true, nil
		}
	}
	return false, nil
}

// selfUpdateFailed counts a failed update of ref and tells the owner, with the end of the log.
func (e *Engine) selfUpdateFailed(ctx context.Context, project, ref, logPath, why string) {
	e.selfUpd.fail(ref, 1)
	tail := redactSecrets(logTail(logPath), os.Environ())
	e.selfUpdateLog(logPath, "update to "+ref+" failed: "+why)
	e.recordSelfUpdateFailure(ctx, project, ref, "self-update to "+shortSHA(ref)+" failed: "+why)
	body := fmt.Sprintf("Update to %s %s (failure %d of %d).", shortSHA(ref), why, e.selfUpd.failures(ref), selfUpdateMaxFailures)
	if tail != "" {
		body += "\n\n" + tail
	}
	e.d.Bus.Emit(ctx, domain.EvSystem, project, "", "", "self-update to "+shortSHA(ref)+" failed", nil)
	e.notify(ctx, notify.PriorityHigh, "onett self-update failed", body, "")
}

// updaterCommand builds the updater: the command run by sh, in its own session so a launchd
// stop of the daemon, which signals the daemon's process group, does not reach it. It gets onett's
// own environment (it is the owner's script, not an agent) plus ONETT_UPDATE_REF, the data
// directory, and none of the agent run variables.
func updaterCommand(command, ref, dataDir string, out *os.File) *exec.Cmd {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dataDir
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ONETT_OUTBOX=") || strings.HasPrefix(kv, "ONETT_UPDATE_REF=") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Env = append(cmd.Env, "ONETT_UPDATE_REF="+ref, "ONETT_DATA_DIR="+dataDir)
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

// startDetached starts cmd and returns its PID and a function that waits for it to exit.
func startDetached(cmd *exec.Cmd) (int, func() error, error) {
	if err := cmd.Start(); err != nil {
		return 0, nil, err
	}
	return cmd.Process.Pid, cmd.Wait, nil
}

// updaterRunning reports whether the lock file names a live process. A lock whose process is gone
// is stale and is removed.
func updaterRunning(lock string) bool {
	b, err := os.ReadFile(lock)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err == nil && pid > 0 {
		// Signal 0 only checks existence. EPERM means the process exists but is not ours.
		if err := syscall.Kill(pid, 0); err == nil || errors.Is(err, syscall.EPERM) {
			return true
		}
	}
	_ = os.Remove(lock)
	return false
}

// versionRev returns the revision a build version names, for git. Builds are stamped by
// `git describe --tags --always --dirty`: "<short sha>", "<tag>-<n>-g<short sha>", either with
// "-dirty". For both it is the short commit; a build made exactly on a tag is stamped with the
// tag alone, and the tag is returned.
func versionRev(version string) string {
	v := strings.TrimSuffix(version, "-dirty")
	if i := strings.LastIndex(v, "-g"); i >= 0 {
		v = v[i+2:]
	}
	return v
}

// versionHasRef reports whether the build version names the commit ref.
func versionHasRef(version, ref string) bool {
	v := versionRev(version)
	return len(v) >= 7 && strings.HasPrefix(ref, v)
}

func shortSHA(ref string) string { return ref[:min(len(ref), 7)] }

// logTail returns the last lines of the file, or "" when it cannot be read.
func logTail(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(b) > 16<<10 {
		b = b[len(b)-16<<10:]
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > selfUpdateTailLines {
		lines = lines[len(lines)-selfUpdateTailLines:]
	}
	s := strings.Join(lines, "\n")
	if len(s) > selfUpdateTailBytes {
		s = s[len(s)-selfUpdateTailBytes:]
	}
	return s
}

// reportSelfUpdate runs once at startup. If update.sh left a result file, it tells the owner how
// the update went and deletes the file. An update that did not come up healthy and was rolled
// back is not tried again for that commit.
func (e *Engine) reportSelfUpdate(ctx context.Context) {
	cfg := e.Config()
	path := filepath.Join(cfg.Server.DataDir, selfUpdateResultFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_ = os.Remove(path)
	res := map[string]string{}
	for line := range strings.SplitSeq(string(b), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			res[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	logPath := filepath.Join(cfg.Server.DataDir, selfUpdateLogFile)
	switch res["status"] {
	case "ok":
		if res["new"] == "" || res["new"] != e.d.Version {
			return
		}
		msg := fmt.Sprintf("onett updated to %s (was %s)", res["new"], res["old"])
		e.selfUpdateLog(logPath, fmt.Sprintf("update ok: now running %s (was %s)", res["new"], res["old"]))
		e.d.Bus.Emit(ctx, domain.EvSystem, cfg.SelfProject, "", "", msg, nil)
		e.notify(ctx, notify.PriorityNormal, msg, "", "")
	case "rolled_back":
		if ref := res["ref"]; ref != "" {
			e.selfUpd.fail(ref, selfUpdateMaxFailures)
		}
		msg := fmt.Sprintf("onett self-update to %s did not come up healthy; rolled back to %s", res["new"], res["old"])
		if reason := res["reason"]; reason != "" {
			msg = fmt.Sprintf("onett self-update to %s was rolled back to %s: %s", res["new"], res["old"], reason)
		}
		e.selfUpdateLog(logPath, strings.TrimPrefix(msg, "onett "))
		e.recordSelfUpdateFailure(ctx, cfg.SelfProject, cmp.Or(res["ref"], res["new"], "unknown"), msg)
		e.d.Bus.Emit(ctx, domain.EvSystem, cfg.SelfProject, "", "", msg, nil)
		e.notify(ctx, notify.PriorityHigh, msg, "See "+selfUpdateLogFile+" in the data directory. It will not be tried again for this commit.", "")
	}
}
