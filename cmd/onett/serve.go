package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/attach"
	"github.com/bobbyrc/onett/internal/auth"
	"github.com/bobbyrc/onett/internal/channel"
	_ "github.com/bobbyrc/onett/internal/channel/all"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/deploy"
	"github.com/bobbyrc/onett/internal/engine"
	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/harness"
	"github.com/bobbyrc/onett/internal/notify"
	"github.com/bobbyrc/onett/internal/observe"
	"github.com/bobbyrc/onett/internal/runshim"
	"github.com/bobbyrc/onett/internal/scm"
	"github.com/bobbyrc/onett/internal/scm/github"
	"github.com/bobbyrc/onett/internal/secrets"
	"github.com/bobbyrc/onett/internal/store/sqlite"
	"github.com/bobbyrc/onett/internal/sysproc"
	"github.com/bobbyrc/onett/internal/workspace"

	// Harness adapters register themselves in init; serve must know every built-in type.
	_ "github.com/bobbyrc/onett/internal/harness/claude"
	_ "github.com/bobbyrc/onett/internal/harness/codex"
	_ "github.com/bobbyrc/onett/internal/harness/exec"
	_ "github.com/bobbyrc/onett/internal/harness/fake"
)

// shutdownTimeout bounds how long in-flight HTTP requests get after Ctrl-C.
const shutdownTimeout = 10 * time.Second

func cmdServe(e *env, args []string) error {
	fs := newFlagSet("serve", e)
	cfgFlag := fs.String("config", "", "config file (default $ONETT_CONFIG or ~/.config/onett/onett.yaml)")
	addrFlag := fs.String("addr", "", "listen address for the starter config written on first run (ignored when a config exists)")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	if *addrFlag != "" {
		if _, _, err := net.SplitHostPort(*addrFlag); err != nil {
			return usagef("--addr %q: %v", *addrFlag, err)
		}
	}
	path, err := configPath(*cfgFlag)
	if err != nil {
		return err
	}

	dir, err := userConfigDir()
	if err != nil {
		return err
	}
	secretStore := secrets.New(filepath.Join(dir, "secrets.env"))
	if err := secretStore.Load(); err != nil {
		return err
	}

	// First run: no config yet. Write the starter one so the owner can finish setup in the
	// web UI. A file that exists but is broken still fails in loadConfig.
	starter, err := writeStarterConfig(path, *addrFlag)
	if err != nil {
		return err
	}
	if starter {
		fmt.Fprintf(e.errOut, "no config found: wrote a starter config to %s\n", path)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	known := harness.Types()
	for name, h := range cfg.Harness {
		if !containsString(known, h.Type) {
			return fmt.Errorf("harness %q has unknown type %q (known: %s)", name, h.Type, strings.Join(known, ", "))
		}
	}

	if err := os.MkdirAll(cfg.Server.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	token, err := ensureToken(cfg.Server.DataDir)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(e.errOut, &slog.HandlerOptions{Level: slog.LevelInfo}))
	for _, w := range cfg.Warnings {
		logger.Warn(w)
	}

	st, err := sqlite.Open(filepath.Join(cfg.Server.DataDir, "onett.db"))
	if err != nil {
		return err
	}
	bus := events.NewBus(st, logger)

	// Resolve the channels before anything starts, so a missing token stops startup.
	chanEnv := channel.Env{DataDir: cfg.Server.DataDir, Getenv: os.Getenv, Logger: logger}
	skipApprovals, err := channel.Resolve(cfg.Channels, chanEnv)
	if err != nil {
		_ = st.Close()
		return err
	}
	fanout, err := notify.NewFanout(notifyConfigs(cfg.Notify, skipApprovals), logger)
	if err != nil {
		_ = st.Close()
		return err
	}
	// The engine sends through notifier; a reload swaps the fanout behind it.
	notifier := notify.NewSwappable(fanout)
	var sup atomic.Pointer[channel.Supervisor]

	exe, err := os.Executable()
	if err != nil {
		_ = st.Close()
		return fmt.Errorf("find onett binary: %w", err)
	}

	inferSelfUpdate(cfg, path, exe)
	// liveCfg is the config harnessWithShim reads limits.run_shim from; a reload replaces it.
	var liveCfg atomic.Pointer[config.Config]
	liveCfg.Store(cfg)
	agentEnv, orchestratorEnv := serveEnvs(exe, cfg.Server.Addr, token)
	attachments := attach.New(st, filepath.Join(cfg.Server.DataDir, "attachments"), func() int64 {
		return int64(liveCfg.Load().Chat.Attachments.MaxSize)
	})
	eng, err := engine.New(engine.Deps{
		Config:      cfg,
		Store:       st,
		Bus:         bus,
		Notifier:    notifier,
		Workspaces:  workspace.New(filepath.Join(cfg.Server.DataDir, "worktrees")),
		Attachments: attachments,
		Logger:      logger,
		SCMFor:      scmFor,
		HarnessFor: func(name string, h config.Harness) (harness.Harness, error) {
			return harnessWithShim(name, h, liveCfg.Load(), exe)
		},
		DeployerFor:  deployerFor,
		ObserversFor: observersFor,
		ReloadConfig: func(p string) (*config.Config, error) {
			// Re-read secrets.env first: ${NAME} references expand from the environment.
			if err := secretStore.Load(); err != nil {
				return nil, err
			}
			c, err := config.Load(p)
			if err != nil {
				return nil, err
			}
			for _, w := range c.Warnings {
				logger.Warn(w)
			}
			inferSelfUpdate(c, p, exe)
			return c, nil
		},
		AfterReload: func(_ context.Context, c *config.Config) error {
			liveCfg.Store(c)
			s := sup.Load()
			if s == nil {
				return nil // still starting; the first start reads the current config
			}
			return applyReload(c, s, chanEnv, notifier, logger)
		},
		Version:         version,
		AgentEnv:        agentEnv,
		OrchestratorEnv: orchestratorEnv,
	})
	if err != nil {
		_ = st.Close()
		return err
	}

	// POST /restart signals this channel; the main loop below then shuts down and restarts.
	restartCh := make(chan struct{}, 1)
	handler := api.NewServer(api.Options{
		Store:            st,
		Bus:              bus,
		Controller:       eng,
		Token:            token,
		Version:          version,
		SecureCookies:    strings.HasPrefix(cfg.Server.PublicURL, "https://"),
		AllowedHostsFunc: func() []string { return serveAllowedHosts(eng.Config().Server) },
		Secrets:          secretStore,
		Attachments:      attachments,
		ChannelTest: func(ctx context.Context, ch config.Channel) error {
			sup := sup.Load()
			if sup == nil {
				return fmt.Errorf("%w: the daemon is still starting", channel.ErrNotRunning)
			}
			return sup.Test(ctx, ch, chanEnv)
		},
		ListenAddr: cfg.Server.Addr,
		Restart: func() {
			select {
			case restartCh <- struct{}{}:
			default:
			}
		},
		Logger: logger,
	})
	// Every request context derives from baseCtx. Streaming (SSE) handlers run until their
	// context ends, and http.Server.Shutdown does not end it, so shutdown cancels baseCtx first.
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           handler,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(e.out, "onett %s listening on http://%s — open the UI with `onett open`\n", version, cfg.Server.Addr)
	// Until an owner account exists, the UI's first page creates it, and it needs the setup link
	// that `onett open` builds.
	if n, err := st.CountUsers(context.Background()); err == nil && n == 0 {
		fmt.Fprintln(e.out, "No owner account yet: run `onett open` to create one and finish setup.")
	}

	engineDone := make(chan error, 1)
	go func() { engineDone <- eng.Run(ctx) }()
	host := &channel.EngineHost{
		Controller: eng, Store: st, Bus: bus, Attachments: attachments,
		MaxPerMessage: func() int { return eng.Config().Chat.Attachments.MaxPerMessage },
	}
	supervisor := channel.NewSupervisor(ctx, host, logger)
	if err := supervisor.Reconcile(eng.Config().Channels, chanEnv); err != nil {
		logger.Error("channels not started", "err", err)
	}
	sup.Store(supervisor)
	serverDone := make(chan error, 1)
	go func() { serverDone <- srv.ListenAndServe() }()

	var listenErr error
	restarting := false
	select {
	case <-ctx.Done():
	case <-restartCh:
		restarting = true
		stop()
	case listenErr = <-serverDone:
		// The listener failed (for example the port is in use); stop the engine too.
		stop()
	}

	// End the streaming handlers, then wait for the server and the engine. The store closes
	// last, after nothing can read or write it.
	cancelBase()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("http shutdown", "err", err)
		_ = srv.Close()
	}
	engErr := <-engineDone
	supervisor.Wait()
	closeErr := st.Close()

	switch {
	case listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed):
		return fmt.Errorf("listen on %s: %w", cfg.Server.Addr, listenErr)
	case engErr != nil:
		return engErr
	case restarting && closeErr == nil:
		// Everything is stopped and the store is closed; start again (see restartAfterShutdown).
		stop()
		return restartAfterShutdown(exe, os.Args, os.Environ(), os.Getenv)
	default:
		return closeErr
	}
}

// applyReload brings the chat bots and the notifiers in line with a freshly loaded config. A
// bot or notifier set that cannot be built (an unset token variable, a bad option) is reported
// and the old one keeps running; the error text carries no secret values.
func applyReload(cfg *config.Config, sup *channel.Supervisor, env channel.Env, notifier *notify.Swappable, logger *slog.Logger) error {
	var errs []error
	if err := sup.Reconcile(cfg.Channels, env); err != nil {
		logger.Error("reload: channels unchanged", "err", err)
		errs = append(errs, fmt.Errorf("channels not reloaded: %w", err))
	}
	fanout, err := notify.NewFanout(notifyConfigs(cfg.Notify, sup.SkipApprovals()), logger)
	if err != nil {
		logger.Error("reload: notifiers unchanged", "err", err)
		errs = append(errs, fmt.Errorf("notifiers not reloaded: %w", err))
	} else {
		notifier.Swap(fanout)
	}
	return errors.Join(errs...)
}

// notifyConfigs converts the notify list. A notifier whose type is in skipApprovals (a channel
// of that kind is running) gets skip_approvals so approvals reach the owner only through the bot.
func notifyConfigs(list []config.Notifier, skipApprovals map[string]bool) []notify.Config {
	out := make([]notify.Config, 0, len(list))
	for _, n := range list {
		opts := n.Options
		if skipApprovals[n.Type] {
			opts = make(map[string]any, len(n.Options)+1)
			for k, v := range n.Options {
				opts[k] = v
			}
			opts["skip_approvals"] = true
		}
		out = append(out, notify.Config{Type: n.Type, Options: opts, MinPriority: notify.Priority(n.MinPriority)})
	}
	return out
}

// ensureToken returns the API token in dataDir, creating it (32 random bytes, hex, mode 0600)
// when it does not exist yet.
func ensureToken(dataDir string) (string, error) {
	if t, err := readToken(dataDir); err == nil {
		return t, nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(tokenPath(dataDir), []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write token: %w", err)
	}
	return token, nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// fullAccess reports whether the harness's provider section asks for full access.
func fullAccess(h config.Harness) bool {
	switch h.Type {
	case "claude-code":
		return h.ClaudeFullAccess()
	case "codex":
		return h.CodexFullAccess()
	}
	return false
}

// harnessFor builds the harness for one configured harness block.
func harnessFor(name string, h config.Harness) (harness.Harness, error) {
	return harness.New(harness.Config{
		Name: name, Type: h.Type, Command: h.Command, Args: h.Args, Env: h.Env,
		InheritEnv: h.InheritEnv, Billing: h.Billing, Isolate: h.IsolateOn(),
		FullAccess: fullAccess(h),
	})
}

// harnessWithShim is harnessFor for a daemon: with limits.run_shim on, the harness starts its
// agents behind a per-run shim, which is this same binary run as `onett run-shim`.
func harnessWithShim(name string, h config.Harness, cfg *config.Config, exe string) (harness.Harness, error) {
	hc := harness.Config{
		Name: name, Type: h.Type, Command: h.Command, Args: h.Args, Env: h.Env,
		InheritEnv: h.InheritEnv, Billing: h.Billing, Isolate: h.IsolateOn(),
		FullAccess: fullAccess(h),
	}
	if cfg != nil && cfg.Limits.RunShimOn() {
		hc.Shim = &harness.ShimOptions{
			DataDir: cfg.Server.DataDir,
			// The shim needs none of onett's environment; the agent's own comes in its spec.
			Launch: runshim.Launch{Command: []string{exe, "run-shim"}, Env: sysproc.AgentEnv(nil, nil)},
		}
	}
	return harness.New(hc)
}

// deployerFor builds the project's deployer. A project without a deploy block has none.
func deployerFor(p *config.Project) (deploy.Deployer, error) {
	if p.Deploy == nil {
		return nil, nil
	}
	return deploy.New(deploy.Config{
		Type: p.Deploy.Type, Options: p.Deploy.Options, Dir: p.Path, Env: p.Env,
	})
}

// observersFor builds every observer a project lists, keyed by observer name.
func observersFor(p *config.Project) (map[string]observe.Observer, error) {
	out := make(map[string]observe.Observer, len(p.Observe))
	for _, o := range p.Observe {
		obs, err := observe.New(observe.Config{Name: o.Name, Type: o.Type, Options: o.Options, Env: p.Env})
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", p.Name, err)
		}
		out[o.Name] = obs
	}
	return out, nil
}

// githubRepo matches owner/name, the only repository form pull requests can use.
var githubRepo = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// scmFor returns the project's GitHub SCM. A local-only project (a path, or a repo that is not
// owner/name) has none, so the engine cannot open pull requests for it.
func scmFor(p *config.Project) (scm.SCM, error) {
	if !githubRepo.MatchString(p.Repo) || strings.HasPrefix(p.Repo, ".") {
		return nil, fmt.Errorf("project %s has no GitHub repository; pull requests need one", p.Name)
	}
	return github.New(p.Repo), nil
}

// serveAllowedHosts returns the Host names the API accepts beyond localhost: the bind host, the
// public URL host and server.allowed_hosts. A wildcard bind address (0.0.0.0, ::, or empty) is
// not a name a client can use, so it adds nothing.
func serveAllowedHosts(s config.Server) []string {
	var out []string
	add := func(h string) {
		if ip := net.ParseIP(h); h == "" || ip != nil && ip.IsUnspecified() {
			return
		}
		out = append(out, h)
	}
	if host, _, err := net.SplitHostPort(s.Addr); err == nil {
		add(host)
	}
	if u, err := url.Parse(s.PublicURL); err == nil {
		add(u.Hostname())
	}
	return append(out, s.AllowedHosts...)
}

// signalContext is cancelled by SIGINT or SIGTERM.
// serveEnvs returns the environment added to every agent session and to orchestrator (chat)
// sessions. Workers get nothing: no onett on PATH and no credentials, because a worker that
// could call the API could act on its own work. Only the orchestrator, which runs the onett
// CLI, gets the binary on PATH, the API address and the agent token: the token derived from
// the owner token that is refused a person's decisions (auth.AgentToken). No agent is given
// the owner token.
func serveEnvs(exe, addr, token string) (agent, orchestrator map[string]string) {
	agent = map[string]string{}
	orchestrator = map[string]string{
		"PATH":        filepath.Dir(exe) + string(os.PathListSeparator) + os.Getenv("PATH"),
		"ONETT_API":   config.LocalURL(addr),
		"ONETT_TOKEN": auth.AgentToken(token),
	}
	return agent, orchestrator
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
