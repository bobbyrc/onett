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

	cmd := updaterCommand(su.Command, dep.Ref, dataDir, logFile)
	start := e.d.StartUpdater
	if start == nil {
		start = startDetached
	}
	pid, wait, err := start(cmd)
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
		if werr := wait(); werr != nil {
			e.selfUpdateFailed(bg, p.Name, dep.Ref, logPath, "exited with an error: "+werr.Error())
		}
	}()
	return nil
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
// own environment (it is the owner's script, not an agent) plus ONETT_UPDATE_REF and the data
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
		e.d.Bus.Emit(ctx, domain.EvSystem, cfg.SelfProject, "", "", msg, nil)
		e.notify(ctx, notify.PriorityHigh, msg, "See "+selfUpdateLogFile+" in the data directory. It will not be tried again for this commit.", "")
	}
}
