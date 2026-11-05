package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

const suRef = "0123456789abcdef0123456789abcdef01234567"

// suEnv is a self-update test: an engine whose project demo is the self project, a fake updater
// and a deployment that has been healthy for 20 minutes.
type suEnv struct {
	*lpEnv
	dir   string
	mu    sync.Mutex
	cmds  []*exec.Cmd
	waitC chan error
}

func suNew(t *testing.T) *suEnv {
	t.Helper()
	dir := t.TempDir()
	doc := lpDoc(t, lpPath(t), "    loops:\n      self_update:\n        enabled: true\n        command: ./update.sh\n", "self_project: demo\n")
	doc = strings.Replace(doc, "data_dir: "+doc[strings.Index(doc, "data_dir: ")+len("data_dir: "):strings.Index(doc, "\nprojects:")], "data_dir: "+dir, 1)
	s := &suEnv{lpEnv: lpNewEngine(t, doc), dir: dir, waitC: make(chan error, 1)}
	s.e.d.Version = "v0.1-3-g" + suRef[:7]
	s.e.d.StartUpdater = func(cmd *exec.Cmd) (int, func() error, error) {
		s.mu.Lock()
		s.cmds = append(s.cmds, cmd)
		s.mu.Unlock()
		return os.Getpid(), func() error { return <-s.waitC }, nil
	}
	s.deploy("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", domain.DeployHealthy, 20*time.Minute)
	return s
}

// deploy stores a release in state, last updated age ago.
func (s *suEnv) deploy(ref string, state domain.DeployState, age time.Duration) {
	s.t.Helper()
	at := s.now.Add(-age)
	if _, err := s.st.CreateDeployment(s.ctx, domain.Deployment{
		Project: "demo", Ref: ref, State: state, StartedAt: at, UpdatedAt: at,
	}); err != nil {
		s.t.Fatal(err)
	}
}

func (s *suEnv) started() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cmds)
}

func TestSelfUpdateStartsDetachedWithRef(t *testing.T) {
	s := suNew(t)
	s.run(loopSelfUpdate)
	if s.started() != 1 {
		t.Fatalf("started %d updaters, want 1", s.started())
	}
	cmd := s.cmds[0]
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid {
		t.Fatalf("updater must run in its own session: %+v", cmd.SysProcAttr)
	}
	env := strings.Join(cmd.Env, "\n")
	if !strings.Contains(env, "\nONETT_UPDATE_REF=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb") {
		t.Fatalf("env lacks ONETT_UPDATE_REF")
	}
	if cmd.Stdout == nil || cmd.Stdout != cmd.Stderr {
		t.Fatalf("stdout and stderr should both go to the log")
	}
	b, _ := os.ReadFile(filepath.Join(s.dir, selfUpdateLockFile))
	if strings.TrimSpace(string(b)) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("lock = %q", b)
	}
}

func TestSelfUpdateHoldsOff(t *testing.T) {
	cases := map[string]func(s *suEnv){
		"healthy too recently": func(s *suEnv) {
			s.deploy("cccccccccccccccccccccccccccccccccccccccc", domain.DeployHealthy, time.Minute)
		},
		"newer release verifying": func(s *suEnv) {
			s.deploy("cccccccccccccccccccccccccccccccccccccccc", domain.DeployVerifying, 0)
		},
		"newer release pending": func(s *suEnv) {
			s.deploy("cccccccccccccccccccccccccccccccccccccccc", domain.DeployPending, 0)
		},
		"newest release failed": func(s *suEnv) {
			s.deploy("cccccccccccccccccccccccccccccccccccccccc", domain.DeployFailed, 0)
		},
		"same version": func(s *suEnv) {
			s.e.d.Version = "v0.1-3-gbbbbbbb-dirty"
		},
		"bare short sha version": func(s *suEnv) {
			s.e.d.Version = "bbbbbbb"
		},
		"dev build": func(s *suEnv) { s.e.d.Version = "dev" },
		"dev build with sha": func(s *suEnv) {
			s.e.d.Version = "dev+0123456789ab"
		},
		"factory paused": func(s *suEnv) { s.e.paused.Store(true) },
		"live lock": func(s *suEnv) {
			_ = os.WriteFile(filepath.Join(s.dir, selfUpdateLockFile), []byte(strconv.Itoa(os.Getpid())), 0o600)
		},
		"three failures": func(s *suEnv) {
			s.e.selfUpd.fail("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", selfUpdateMaxFailures)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			s := suNew(t)
			setup(s)
			// Called directly: a manual run already refuses while the factory is paused.
			if err := s.e.selfUpdateLoop(s.ctx, s.e.Config().Projects["demo"]); err != nil {
				t.Fatal(err)
			}
			if n := s.started(); n != 0 {
				t.Fatalf("started %d updaters, want none", n)
			}
		})
	}
}

func TestSelfUpdateStaleLockProceeds(t *testing.T) {
	s := suNew(t)
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(s.dir, selfUpdateLockFile)
	_ = os.WriteFile(lock, []byte(strconv.Itoa(dead.Process.Pid)), 0o600)
	if err := syscall.Kill(dead.Process.Pid, 0); err == nil {
		t.Skip("pid was reused")
	}
	s.run(loopSelfUpdate)
	if s.started() != 1 {
		t.Fatalf("stale lock should not block; started %d", s.started())
	}
}

func TestSelfUpdateFailureNotifiesAndStopsAfterThree(t *testing.T) {
	s := suNew(t)
	for i := 1; i <= selfUpdateMaxFailures; i++ {
		_ = os.Remove(filepath.Join(s.dir, selfUpdateLockFile))
		_ = os.WriteFile(filepath.Join(s.dir, selfUpdateLogFile), []byte("boom: build failed\n"), 0o600)
		s.run(loopSelfUpdate)
		s.waitC <- os.ErrInvalid
		deadline := time.Now().Add(5 * time.Second)
		for s.e.selfUpd.failures("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb") < i {
			if time.Now().After(deadline) {
				t.Fatalf("failure %d not counted", i)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	_ = os.Remove(filepath.Join(s.dir, selfUpdateLockFile))
	s.run(loopSelfUpdate)
	if s.started() != selfUpdateMaxFailures {
		t.Fatalf("started %d updaters, want %d", s.started(), selfUpdateMaxFailures)
	}
	evs, _ := s.st.ListEvents(s.ctx, store.EventFilter{FromStart: true})
	var high bool
	for _, ev := range evs {
		if ev.Type == domain.EvNotification && strings.Contains(string(ev.Data), `"high"`) && strings.Contains(string(ev.Data), "boom: build failed") {
			high = true
		}
	}
	if !high {
		t.Fatalf("no high-priority notification with the log tail")
	}
}

func TestSelfUpdateReportsResultOnce(t *testing.T) {
	s := suNew(t)
	res := filepath.Join(s.dir, selfUpdateResultFile)
	_ = os.WriteFile(res, []byte("old=v0.1-1-g1111111\nnew="+s.e.d.Version+"\nref="+suRef+"\nstatus=ok\n"), 0o600)
	s.e.reportSelfUpdate(s.ctx)
	if _, err := os.Stat(res); err == nil {
		t.Fatal("result file should be deleted")
	}
	evs, _ := s.st.ListEvents(s.ctx, store.EventFilter{FromStart: true})
	var found bool
	for _, ev := range evs {
		if ev.Type == domain.EvSystem && strings.Contains(ev.Message, "onett updated to "+s.e.d.Version+" (was v0.1-1-g1111111)") {
			found = true
		}
	}
	if !found {
		t.Fatal("no 'onett updated' event")
	}
}

func TestSelfUpdateRolledBackStopsRetrying(t *testing.T) {
	s := suNew(t)
	ref := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	_ = os.WriteFile(filepath.Join(s.dir, selfUpdateResultFile), []byte("old=x\nnew=y\nref="+ref+"\nstatus=rolled_back\n"), 0o600)
	s.e.reportSelfUpdate(s.ctx)
	s.run(loopSelfUpdate)
	if s.started() != 0 {
		t.Fatal("a rolled-back commit must not be retried")
	}
}

func TestVersionHasRef(t *testing.T) {
	for v, want := range map[string]bool{
		"0123456":               true,
		"0123456-dirty":         true,
		"v1.2-4-g0123456":       true,
		"v1.2-4-g0123456-dirty": true,
		"v1.2-4-gabcdef0":       false,
		"v1.2.3":                false,
		"dev":                   false,
		"":                      false,
		"0123":                  false, // too short to be a commit
	} {
		if got := versionHasRef(v, suRef); got != want {
			t.Errorf("versionHasRef(%q) = %v, want %v", v, got, want)
		}
	}
}
