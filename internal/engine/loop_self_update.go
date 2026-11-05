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
	"github.com/bobbyrc/onett/internal/store"
)

// selfUpdateMaxFailures is how many times an update to one commit may fail before the loop stops
// trying that commit. The count is in memory and resets when the daemon restarts.
const selfUpdateMaxFailures = 3

// selfUpdateBusyExit is the exit status update.sh uses when the factory stayed busy for the
// whole drain and it resumed without changing anything (EX_TEMPFAIL).
const selfUpdateBusyExit = 75

// selfUpdateBackoff is how long a commit is left alone after its updater gave up on a busy factory.
const selfUpdateBackoff = time.Hour

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
	// retryAt holds a commit back until then: its updater gave up because the factory stayed
	// busy. Separate from fails, which counts real failures.
	retryAt map[string]time.Time
	// behindSince is when a release different from the running version first became eligible,
	// and behindVersion the running version it was measured for. max_defer counts from here, not
	// from the newest release, so a steady stream of releases cannot keep pushing the update away.
	behindSince   time.Time
	behindVersion string
}

// behind returns when the daemon first fell behind while running version, noting eligible as that
// moment if it had not yet. It starts over when the running version changes.
func (s *selfUpdateState) behind(version string, eligible time.Time) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.behindVersion != version || s.behindSince.IsZero() {
		s.behindVersion, s.behindSince = version, eligible
	}
	return s.behindSince
}

func (s *selfUpdateState) backoff(ref string, until time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retryAt == nil {
		s.retryAt = map[string]time.Time{}
	}
	s.retryAt[ref] = until
}

func (s *selfUpdateState) backedOff(ref string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return now.Before(s.retryAt[ref])
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
	version := e.d.Version
	if version == "" || strings.HasPrefix(version, "dev") {
		return nil // a dev build has no commit to compare with, so it never replaces itself
	}
	if e.projectPaused(p.Name) {
		return nil
	}
	deps, err := e.d.Store.ListDeployments(ctx, p.Name, 1)
	if err != nil {
		return fmt.Errorf("self_update: list deployments: %w", err)
	}
	// The newest release must be healthy: pending, deploying and verifying mean another is in
	// flight, and failed or rolled back means the newest commit is not one to run.
	if len(deps) == 0 || deps[0].State != domain.DeployHealthy {
		return nil
	}
	dep := deps[0]
	if e.d.Now().Sub(dep.UpdatedAt) < su.MinHealthy.Duration {
		return nil
	}
	if versionHasRef(version, dep.Ref) {
		return nil
	}
	if e.selfUpd.failures(dep.Ref) >= selfUpdateMaxFailures {
		return nil
	}

	if e.selfUpd.backedOff(dep.Ref, e.d.Now()) {
		return nil
	}
	// Starting the updater pauses the factory, so prefer a moment when nothing is going on. A
	// busy factory is waited out until max_defer after the daemon first fell behind (not after
	// the newest release, whose clock every new release would restart); then the updater drains
	// whatever is running. The newest healthy release is still the one installed.
	behindSince := e.selfUpd.behind(version, dep.UpdatedAt.Add(su.MinHealthy.Duration))
	if e.d.Now().Sub(behindSince) < su.MaxDefer.Duration {
		busy, err := e.selfUpdateBusy(ctx, p.Name)
		if err != nil {
			return fmt.Errorf("self_update: %w", err)
		}
		if busy {
			return nil
		}
	}

	dataDir := cfg.Server.DataDir
	lock := filepath.Join(dataDir, selfUpdateLockFile)
	if updaterRunning(lock) {
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

	wait := cfg.Limits.RunTimeout.Duration + 5*time.Minute
	cmd := updaterCommand(su.Command, dep.Ref, dataDir, wait, logFile)
	start := e.d.StartUpdater
	if start == nil {
		start = startDetached
	}
	pid, waitCmd, err := start(cmd)
	if err != nil {
		e.selfUpdateFailed(ctx, p.Name, dep.Ref, logPath, "could not start: "+err.Error())
		return nil
	}
	// A failed lock write is not fatal: the loop's own mutex covers this daemon, and the
	// updater's pause makes a second one wait.
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
			if exitCode(werr) == selfUpdateBusyExit {
				e.selfUpd.backoff(dep.Ref, e.d.Now().Add(selfUpdateBackoff))
				e.d.Bus.Emit(bg, domain.EvSystem, p.Name, "", "", "self-update to "+short+" gave up: the factory stayed busy; trying again in 1h", nil)
				return
			}
			e.selfUpdateFailed(bg, p.Name, dep.Ref, logPath, "exited with an error: "+werr.Error())
		}
	}()
	return nil
}

// selfUpdateBusy reports whether a task run is live or a task of the project is queued. Chat and
// goal turns have no task and are short, so they do not count.
func (e *Engine) selfUpdateBusy(ctx context.Context, project string) (bool, error) {
	runs, err := e.d.Store.ListRuns(ctx, store.RunFilter{
		Statuses: []domain.RunStatus{domain.RunStarting, domain.RunRunning}, Limit: 1000,
	})
	if err != nil {
		return false, err
	}
	for _, r := range runs {
		if r.TaskID != "" {
			return true, nil
		}
	}
	queued, err := e.d.Store.ListTasks(ctx, store.TaskFilter{
		Project: project, Statuses: []domain.TaskStatus{domain.TaskQueued}, Limit: 1,
	})
	if err != nil {
		return false, err
	}
	return len(queued) > 0, nil
}

// exitCode is the exit status carried by err, or -1 when it carries none.
func exitCode(err error) int {
	var ee interface{ ExitCode() int }
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
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
// directory and ONETT_UPDATE_WAIT (seconds it may wait for runs to drain), and none of the agent run variables.
func updaterCommand(command, ref, dataDir string, wait time.Duration, out *os.File) *exec.Cmd {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dataDir
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ONETT_OUTBOX=") || strings.HasPrefix(kv, "ONETT_UPDATE_REF=") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Env = append(cmd.Env, "ONETT_UPDATE_REF="+ref, "ONETT_DATA_DIR="+dataDir,
		"ONETT_UPDATE_WAIT="+strconv.Itoa(int(wait.Seconds())))
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
