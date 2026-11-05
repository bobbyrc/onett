package engine

import (
	"context"
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

// selfUpdateState is what the self_update loop remembers between ticks.
type selfUpdateState struct {
	mu    sync.Mutex
	fails map[string]int // failed attempts by full commit SHA
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

// selfUpdateLoop updates the running daemon to the self project's latest release once that
// release has been healthy on staging for min_healthy and the daemon is not already that commit.
// Code decides when; the configured command does the work (for the dogfood setup,
// examples/dogfood/update.sh). The command is started detached and not waited for: it restarts
// this daemon, so it has to outlive it.
func (e *Engine) selfUpdateLoop(ctx context.Context, p *config.Project) error {
	cfg := e.Config()
	su := cfg.LoopsFor(p.Name).SelfUpdate
	if su.Command == "" {
		return errors.New("self_update: no command configured")
	}
	dep, ok, err := e.selfUpdateEligible(ctx, p)
	if err != nil {
		return err
	}
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

// selfUpdateScan is how many recent deployments selfUpdateTarget looks through.
const selfUpdateScan = 50

// selfUpdateTarget picks the release to update to: the newest one that is healthy, has been for
// min_healthy, and has not failed too often. Newer releases do not hide it, because with merges
// landing every few minutes the newest one is rarely old enough. The scan stops at the release the
// daemon already runs, since anything older is not an update.
func (e *Engine) selfUpdateTarget(ctx context.Context, p *config.Project, minHealthy time.Duration, version string) (domain.Deployment, bool, error) {
	deps, err := e.d.Store.ListDeployments(ctx, p.Name, selfUpdateScan) // newest first
	if err != nil {
		return domain.Deployment{}, false, fmt.Errorf("self_update: list deployments: %w", err)
	}
	now := e.d.Now()
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
		return d, true, nil
	}
	return domain.Deployment{}, false, nil
}

// selfUpdateEligible returns the release the daemon should update to now, if any. Both the
// self_update loop and the deploy loop's hold-back use it, so they cannot disagree. A dev build
// never replaces itself, and a paused project does not update.
func (e *Engine) selfUpdateEligible(ctx context.Context, p *config.Project) (domain.Deployment, bool, error) {
	su := e.Config().LoopsFor(p.Name).SelfUpdate
	version := e.d.Version
	if version == "" || strings.HasPrefix(version, "dev") || e.projectPaused(p.Name) {
		return domain.Deployment{}, false, nil
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
	_, ok, err := e.selfUpdateEligible(ctx, p)
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

// versionHasRef reports whether the build version names the commit ref. Builds are stamped by
// `git describe --tags --always --dirty`: "<short sha>", "<tag>-<n>-g<short sha>", either with
// "-dirty".
func versionHasRef(version, ref string) bool {
	v := strings.TrimSuffix(version, "-dirty")
	if i := strings.LastIndex(v, "-g"); i >= 0 {
		v = v[i+2:]
	}
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
	switch res["status"] {
	case "ok":
		if res["new"] == "" || res["new"] != e.d.Version {
			return
		}
		msg := fmt.Sprintf("onett updated to %s (was %s)", res["new"], res["old"])
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
		e.d.Bus.Emit(ctx, domain.EvSystem, cfg.SelfProject, "", "", msg, nil)
		e.notify(ctx, notify.PriorityHigh, msg, "See "+selfUpdateLogFile+" in the data directory. It will not be tried again for this commit.", "")
	}
}
