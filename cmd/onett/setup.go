package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/bobbyrc/onett/internal/config"
)

// projectName matches names usable as YAML keys and in CLI flags.
var projectName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// selfRepo is onett's own repository; a project for it becomes self_project.
const selfRepo = "bobbyrc/onett"

func cmdInit(e *env, args []string) error {
	fs := newFlagSet("init", e)
	cfgFlag := fs.String("config", "", "config file to write (default $ONETT_CONFIG or ~/.config/onett/onett.yaml)")
	project := fs.String("project", "", "add this project to the config")
	repo := fs.String("repo", "", "the project's GitHub repository, owner/name (with --project)")
	dir := fs.String("path", "", "the project's local clone (with --project; default <data_dir>/repos/<name>)")
	notifyFlag := fs.String("notify", "", "how to reach you: ntfy (needs --ntfy-topic)")
	ntfyTopic := fs.String("ntfy-topic", "", "the ntfy topic to publish to (with --notify ntfy)")
	ntfyServer := fs.String("ntfy-server", "", "a self-hosted ntfy server URL (with --notify ntfy; default https://ntfy.sh)")
	channelFlag := fs.String("channel", "", "a chat channel to talk to onett: telegram or discord (needs --user-ids)")
	userIDs := fs.String("user-ids", "", "your user IDs on the channel, comma separated (with --channel)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "init [--config P] [--project NAME --repo OWNER/NAME [--path DIR]] [--notify ntfy --ntfy-topic T [--ntfy-server URL]] [--channel telegram|discord --user-ids IDS]"); err != nil {
		return err
	}
	if (*project == "") != (*repo == "") {
		return usagef("--project and --repo go together")
	}
	if *project == "" && *dir != "" {
		return usagef("--path needs --project")
	}
	if *project != "" && !projectName.MatchString(*project) {
		return usagef("project name %q may use letters, digits, '.', '_' and '-'", *project)
	}
	notify, err := notifyFromFlags(*notifyFlag, *ntfyTopic, *ntfyServer, *channelFlag, *userIDs)
	if err != nil {
		return err
	}
	path, err := configPath(*cfgFlag)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite it", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	offered := false
	if !notify.set() && e.isTTY && e.in != nil {
		offered = true
		if notify, err = askNotify(e); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	_, werr := f.WriteString(initialConfigWith(*project, *repo, *dir, notify))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("write %s: %w", path, werr)
	}

	serveHint := "onett serve"
	if *cfgFlag != "" {
		serveHint += " --config " + *cfgFlag
	}
	fmt.Fprintf(e.out, "Wrote %s\n", path)
	if err := reportNotify(e, notify, offered); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "\nNext steps:\n")
	fmt.Fprintf(e.out, "  %s    start the daemon\n", serveHint)
	fmt.Fprintln(e.out, "  onett open     open the web UI in a browser")
	if *repo == selfRepo {
		fmt.Fprintln(e.out, "\nSelf-update is on by default for this project once its command is known;")
		fmt.Fprintln(e.out, "set loops.self_update.enabled: false to turn it off (see the comments in the file).")
	}
	fmt.Fprintln(e.out, "\nonett.example.yaml in the onett repository lists every option.")
	return nil
}

// initialConfig is the starter onett.yaml. Every key it leaves out has a default.
func initialConfig(project, repo, dir string) string {
	return initialConfigWith(project, repo, dir, notifySetup{})
}

// notifySetup is how the owner chose to be reached. It holds no secrets: tokens are named by
// environment variable and live in secrets.env.
type notifySetup struct {
	ntfyTopic, ntfyServer string
	channel               string // "", "telegram" or "discord"
	userIDs               []string
}

func (n notifySetup) set() bool { return n.ntfyTopic != "" || n.channel != "" }

// channelTokenEnv is the environment variable the bot token of a channel is read from.
func channelTokenEnv(channel string) string { return strings.ToUpper(channel) + "_BOT_TOKEN" }

// notifyFromFlags validates the --notify and --channel flags.
func notifyFromFlags(notify, topic, server, channel, ids string) (notifySetup, error) {
	var n notifySetup
	switch notify {
	case "":
		if topic != "" || server != "" {
			return n, usagef("--ntfy-topic and --ntfy-server need --notify ntfy")
		}
	case "ntfy":
		if topic == "" {
			return n, usagef("--notify ntfy needs --ntfy-topic")
		}
		n.ntfyTopic, n.ntfyServer = topic, server
	default:
		return n, usagef("--notify %q: only ntfy is supported (use --channel for Telegram or Discord)", notify)
	}
	switch channel {
	case "":
		if ids != "" {
			return n, usagef("--user-ids needs --channel")
		}
	case "telegram", "discord":
		list, err := parseUserIDs(channel, ids)
		if err != nil {
			return n, err
		}
		n.channel, n.userIDs = channel, list
	default:
		return n, usagef("--channel %q: use telegram or discord", channel)
	}
	return n, nil
}

// parseUserIDs splits a comma separated list and checks each ID is digits (Telegram IDs must
// also fit an int64).
func parseUserIDs(channel, s string) ([]string, error) {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, err := strconv.ParseUint(p, 10, 63); err != nil {
			return nil, usagef("%s user ID %q must be a whole number", channel, p)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, usagef("--channel %s needs --user-ids", channel)
	}
	return out, nil
}

// askChannel asks for the user IDs a chat channel will obey. Blank means skip.
func askChannel(ask func(string) string, channel, hint string) (notifySetup, error) {
	ids := ask(fmt.Sprintf("Your %s user ID, digits only (%s); several separated by commas: ", channel, hint))
	if ids == "" {
		return notifySetup{}, nil
	}
	list, err := parseUserIDs(channel, ids)
	if err != nil {
		return notifySetup{}, err
	}
	return notifySetup{channel: channel, userIDs: list}, nil
}

// askNotify asks once how onett should reach the owner, then only for the non-secret values
// the choice needs. An unreadable or unrecognised answer means skip.
func askNotify(e *env) (notifySetup, error) {
	r := bufio.NewReader(e.in)
	ask := func(q string) string {
		fmt.Fprint(e.out, q)
		line, _ := r.ReadString('\n')
		return strings.TrimSpace(line)
	}
	var n notifySetup
	fmt.Fprintln(e.out, "How should onett reach you when it needs you?")
	fmt.Fprintln(e.out, "  [1] Telegram  [2] Discord  [3] ntfy  [4] skip (log only: you will not be told)")
	switch ask("Choice [4]: ") {
	case "1":
		return askChannel(ask, "telegram", "message @userinfobot in Telegram to find it")
	case "2":
		return askChannel(ask, "discord", "turn on Developer Mode, then right-click yourself and choose Copy User ID")
	case "3":
		topic := ask("ntfy topic (anyone who knows it can read it; blank makes a random one): ")
		if topic == "" {
			b := make([]byte, 8)
			if _, err := rand.Read(b); err != nil {
				return n, err
			}
			topic = "onett-" + hex.EncodeToString(b)
		}
		n.ntfyTopic = topic
		n.ntfyServer = ask("ntfy server URL (blank for https://ntfy.sh): ")
	}
	return n, nil
}

func initialConfigWith(project, repo, dir string, n notifySetup) string {
	var b strings.Builder
	b.WriteString("# onett.yaml: the daemon's settings. Every key is optional; unset keys take the\n")
	b.WriteString("# defaults. onett.example.yaml in the onett repository lists every option.\n\n")
	b.WriteString("server:\n")
	b.WriteString("  addr: 127.0.0.1:7777      # where the API and web UI listen\n")
	b.WriteString("  # data_dir defaults to ~/.local/share/onett (database, token, clones, worktrees)\n\n")
	b.WriteString("notify:\n")
	b.WriteString("  - type: log               # write notifications to the daemon log\n")
	if n.ntfyTopic != "" {
		b.WriteString("  - type: ntfy              # push notifications to your phone\n")
		b.WriteString("    options:\n")
		fmt.Fprintf(&b, "      topic: %s\n", strconv.Quote(n.ntfyTopic))
		if n.ntfyServer != "" {
			fmt.Fprintf(&b, "      server: %s\n", strconv.Quote(n.ntfyServer))
		}
		b.WriteString("      # token_env: NTFY_TOKEN   # for a private topic: the variable's name, the token goes in secrets.env\n")
	}
	if n.channel != "" {
		b.WriteString("\nchannels:\n")
		fmt.Fprintf(&b, "  - type: %s\n    options:\n", n.channel)
		fmt.Fprintf(&b, "      bot_token_env: %s   # the variable's name; the token goes in secrets.env\n", channelTokenEnv(n.channel))
		if n.channel == "telegram" {
			fmt.Fprintf(&b, "      allowed_user_ids: [%s]\n", strings.Join(n.userIDs, ", "))
		} else {
			q := make([]string, len(n.userIDs))
			for i, id := range n.userIDs {
				q[i] = strconv.Quote(id)
			}
			fmt.Fprintf(&b, "      allowed_user_ids: [%s]\n", strings.Join(q, ", "))
		}
	}
	if project != "" {
		b.WriteString("\n")
		if repo == selfRepo {
			fmt.Fprintf(&b, "self_project: %s           # onett itself: friction is filed here\n\n", project)
			b.WriteString("# Self-update is on by default for onett's own project once a command is known: the daemon\n")
			b.WriteString("# then moves itself to each release that has been healthy on staging. Turn it off with\n")
			b.WriteString("# `enabled: false`. With the binary at <dir>/onett, this file at <dir>/onett.yaml, data_dir\n")
			b.WriteString("# <dir>/data, mise on PATH and a clone with examples/dogfood/update.sh, onett works out the\n")
			b.WriteString("# command itself; otherwise set it (see examples/dogfood/README.md).\n")
			b.WriteString("# loops:\n#   self_update:\n#     enabled: false\n#     command: DOGFOOD=<dir> mise exec -- <clone>/examples/dogfood/update.sh\n\n")
		}
		b.WriteString("projects:\n")
		fmt.Fprintf(&b, "  %s:\n", project)
		fmt.Fprintf(&b, "    repo: %s\n", strconv.Quote(repo))
		b.WriteString("    # policy: supervised   # opt in: medium- and high-risk merges wait for you\n")
		if dir != "" {
			fmt.Fprintf(&b, "    path: %s\n", strconv.Quote(dir))
		}
	}
	return b.String()
}

func cmdToken(e *env, args []string) error {
	fs := newFlagSet("token", e)
	cfgFlag := fs.String("config", "", "config file (default $ONETT_CONFIG or ~/.config/onett/onett.yaml)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "token [--config P]"); err != nil {
		return err
	}
	path, err := configPath(*cfgFlag)
	if err != nil {
		return err
	}
	cfg, cfgErr := loadConfig(path)
	if cfgErr != nil && os.Getenv("ONETT_TOKEN") == "" {
		return cfgErr
	}
	token, err := apiToken(cfg)
	if err != nil {
		return err
	}
	fmt.Fprintln(e.out, token)
	return nil
}

func cmdOpen(e *env, args []string) error {
	fs := newFlagSet("open", e)
	cfgFlag := fs.String("config", "", "config file (default $ONETT_CONFIG or ~/.config/onett/onett.yaml)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "open [--config P]"); err != nil {
		return err
	}
	path, err := configPath(*cfgFlag)
	if err != nil {
		return err
	}
	// The browser signs in with a password, so no token goes in the URL. The config is only
	// needed for the address, which ONETT_API can give instead.
	cfg, cfgErr := loadConfig(path)
	if cfgErr != nil && os.Getenv("ONETT_API") == "" {
		return cfgErr
	}
	url := strings.TrimRight(publicBaseURL(cfg), "/") + "/"

	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	if err := exec.Command(opener, url).Run(); err != nil {
		fmt.Fprintf(e.out, "Could not open a browser (%v). Open this URL:\n%s\n", err, url)
	}
	return nil
}

// publicBaseURL is the address the web UI is reached at: $ONETT_API, then the config's
// public_url, then the default address.
func publicBaseURL(cfg *config.Config) string {
	if v := os.Getenv("ONETT_API"); v != "" {
		return v
	}
	if cfg != nil && cfg.Server.PublicURL != "" {
		return cfg.Server.PublicURL
	}
	return apiBaseURL(cfg)
}

// inferSelfUpdate fills in loops.self_update.command from the running binary and config when
// they follow the dogfood layout (see config.InferSelfUpdateCommand). exe "" means this
// process's own binary. Failing to find either leaves self-update as configured.
func inferSelfUpdate(cfg *config.Config, configPath, exe string) {
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return
		}
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if abs, err := filepath.Abs(configPath); err == nil {
		configPath = abs
	}
	cfg.ApplySelfUpdateInference(configPath, exe, exec.LookPath)
}

// reportNotify tells the owner what was set up and which secret to provide, or, when nothing
// was, that onett cannot reach them. For a channel it also creates secrets.env with an empty
// line for the token (mode 600), unless the file exists. It never prints or stores a secret.
func reportNotify(e *env, n notifySetup, offered bool) error {
	switch {
	case n.channel != "":
		key := channelTokenEnv(n.channel)
		dir, err := userConfigDir()
		if err != nil {
			return err
		}
		secrets := filepath.Join(dir, "secrets.env")
		created, err := ensureSecretsKey(secrets, key)
		if err != nil {
			return err
		}
		fmt.Fprintf(e.out, "\nSet up a %s channel. Put the bot token in %s as:\n  %s=...\n", n.channel, secrets, key)
		if created {
			fmt.Fprintf(e.out, "(created that file with an empty %s line, mode 600; or use the Secrets page in the web UI)\n", key)
		} else {
			fmt.Fprintln(e.out, "(the file already exists and was not changed; or use the Secrets page in the web UI)")
		}
		fmt.Fprintf(e.out, "How to get the token: docs/channels.md, section %s.\n", strings.ToUpper(n.channel[:1])+n.channel[1:])
	case n.ntfyTopic != "":
		fmt.Fprintf(e.out, "\nSet up ntfy on topic %q: subscribe to it in the ntfy app. For a private topic, set\ntoken_env in the notifier and put NTFY_TOKEN=... in secrets.env.\n", n.ntfyTopic)
	default:
		fmt.Fprintln(e.out, "\nonett will not be able to tell you when a goal needs you; run `onett doctor` for how to fix this.")
		if !offered {
			fmt.Fprintln(e.out, "To fix it now, run `onett init` in a terminal, or pass --notify ntfy --ntfy-topic T or --channel telegram|discord --user-ids IDS.")
		}
	}
	return nil
}

// ensureSecretsKey creates path (mode 600) with an empty line for key. An existing file is
// never touched.
func ensureSecretsKey(path, key string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, werr := f.WriteString(key + "=\n")
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr == nil, werr
}
