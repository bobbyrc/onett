package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/channel"
	_ "github.com/bobbyrc/onett/internal/channel/all"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/deploy"
	"github.com/bobbyrc/onett/internal/engine"
	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/harness"
	"github.com/bobbyrc/onett/internal/notify"
	"github.com/bobbyrc/onett/internal/observe"
	"github.com/bobbyrc/onett/internal/scm"
	"github.com/bobbyrc/onett/internal/scm/github"
	"github.com/bobbyrc/onett/internal/store/sqlite"
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
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	path, err := configPath(*cfgFlag)
	if err != nil {
		return err
	}

	dir, err := userConfigDir()
	if err != nil {
		return err
	}
	if err := loadSecretsFile(filepath.Join(dir, "secrets.env")); err != nil {
		return err
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

	st, err := sqlite.Open(filepath.Join(cfg.Server.DataDir, "onett.db"))
	if err != nil {
		return err
	}
	bus := events.NewBus(st, logger)

	// Resolve the channels before anything starts, so a missing token stops startup.
	starters, skipApprovals, err := resolveChannels(cfg, logger)
	if err != nil {
		_ = st.Close()
		return err
	}
	notifCfgs := notifyConfigs(cfg.Notify, skipApprovals)
	notifier, err := notify.NewFanout(notifCfgs, logger)
	if err != nil {
		_ = st.Close()
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		_ = st.Close()
		return fmt.Errorf("find onett binary: %w", err)
	}

	inferSelfUpdate(cfg, path, exe)
	agentEnv, orchestratorEnv := serveEnvs(exe, cfg.Server.Addr, token)
	eng, err := engine.New(engine.Deps{
		Config:       cfg,
		Store:        st,
		Bus:          bus,
		Notifier:     notifier,
		Workspaces:   workspace.New(filepath.Join(cfg.Server.DataDir, "worktrees")),
		Logger:       logger,
		SCMFor:       scmFor,
		HarnessFor:   harnessFor,
		DeployerFor:  deployerFor,
		ObserversFor: observersFor,
		ReloadConfig: func(p string) (*config.Config, error) {
			c, err := config.Load(p)
			if err == nil {
				inferSelfUpdate(c, p, exe)
			}
			return c, err
		},
		Version:         version,
		AgentEnv:        agentEnv,
		OrchestratorEnv: orchestratorEnv,
	})
	if err != nil {
		_ = st.Close()
		return err
	}

	handler := api.NewServer(api.Options{
		Store:         st,
		Bus:           bus,
		Controller:    eng,
		Token:         token,
		Version:       version,
		SecureCookies: strings.HasPrefix(cfg.Server.PublicURL, "https://"),
		AllowedHosts:  serveAllowedHosts(cfg.Server),
		Logger:        logger,
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

	engineDone := make(chan error, 1)
	go func() { engineDone <- eng.Run(ctx) }()
	var bots sync.WaitGroup
	host := &channel.EngineHost{Controller: eng, Store: st, Bus: bus}
	startBots(ctx, &bots, host, starters, logger)
	serverDone := make(chan error, 1)
	go func() { serverDone <- srv.ListenAndServe() }()

	var listenErr error
	select {
	case <-ctx.Done():
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
	bots.Wait()
	closeErr := st.Close()

	switch {
	case listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed):
		return fmt.Errorf("listen on %s: %w", cfg.Server.Addr, listenErr)
	case engErr != nil:
		return engErr
	default:
		return closeErr
	}
}

// startBots builds and runs each channel's bot. A bot failure must not take the daemon down, so
// it is logged and the others carry on. bots counts the running bots so shutdown can wait.
func startBots(ctx context.Context, bots *sync.WaitGroup, host channel.Host, starters []namedStarter, logger *slog.Logger) {
	for _, s := range starters {
		bot, err := s.start(host)
		if err != nil {
			logger.Error(s.name+" channel not started", "err", err)
			continue
		}
		bots.Add(1)
		go func() {
			defer bots.Done()
			if err := bot.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error(s.name+" channel stopped", "err", err)
			}
		}()
	}
}

// namedStarter is a channel's Starter with the type it came from, for log messages.
type namedStarter struct {
	name  string
	start channel.Starter
}

// resolveChannels validates every configured channel and resolves its token, so a bad config or
// an empty token variable stops startup. Only the first channel of each type is used. It also
// returns the notifier types that must not send approval requests while a channel runs.
func resolveChannels(cfg *config.Config, logger *slog.Logger) ([]namedStarter, map[string]bool, error) {
	env := channel.Env{DataDir: cfg.Server.DataDir, Getenv: os.Getenv, Logger: logger}
	var starters []namedStarter
	skip := map[string]bool{}
	seen := map[string]bool{}
	for _, ch := range cfg.Channels {
		if seen[ch.Type] {
			continue
		}
		seen[ch.Type] = true
		reg, ok := channel.Lookup(ch.Type)
		if !ok {
			return nil, nil, fmt.Errorf("channel %q: unknown type (known: %s)", ch.Type, strings.Join(channel.Types(), ", "))
		}
		start, err := reg.Factory(ch, env)
		if err != nil {
			return nil, nil, err
		}
		starters = append(starters, namedStarter{name: ch.Type, start: start})
		if reg.SkipApprovalsForNotifier != "" {
			skip[reg.SkipApprovalsForNotifier] = true
		}
	}
	return starters, skip, nil
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

// harnessFor builds the harness for one configured harness block.
func harnessFor(name string, h config.Harness) (harness.Harness, error) {
	return harness.New(harness.Config{
		Name: name, Type: h.Type, Command: h.Command, Args: h.Args, Env: h.Env,
		InheritEnv: h.InheritEnv, Billing: h.Billing,
	})
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

// loadSecretsFile sets environment variables from a secrets file before the config is read,
// because ${NAME} references in the config expand from the environment. A missing file is not
// an error. Variables already set in the environment win.
func loadSecretsFile(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read secrets: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("read secrets: %w", err)
	}
	for _, kv := range parseSecrets(data) {
		if _, set := os.LookupEnv(kv.key); !set {
			if err := os.Setenv(kv.key, kv.value); err != nil {
				return fmt.Errorf("set %s from secrets: %w", kv.key, err)
			}
		}
	}
	return nil
}

// secret is one KEY=value line of secrets.env. The value is never printed.
type secret struct {
	key, value string
}

// parseSecrets reads KEY=value lines. Blank lines and lines starting with # are skipped, an
// optional leading "export " is dropped, and one pair of matching quotes around the value is
// removed. Lines without "=" or with an empty key are ignored.
func parseSecrets(data []byte) []secret {
	var out []secret
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		out = append(out, secret{key: key, value: value})
	}
	return out
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
// serveEnvs returns the environment added to agent sessions and to orchestrator (chat) sessions.
// Every agent gets the onett binary on PATH and no credentials: a worker that could call the
// API could approve its own work. Only the orchestrator, which runs the onett CLI, gets the API
// address and token.
func serveEnvs(exe, addr, token string) (agent, orchestrator map[string]string) {
	agent = map[string]string{
		"PATH": filepath.Dir(exe) + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
	orchestrator = map[string]string{
		"ONETT_API":   config.LocalURL(addr),
		"ONETT_TOKEN": token,
	}
	return agent, orchestrator
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
