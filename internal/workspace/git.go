package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"unicode/utf8"
)

// hardening is put in front of every git command this package runs. A setting given on the
// command line wins over every config file, and git hands these on to the git it starts inside
// a nested repository, so they hold even if a config file changes while a command runs.
var hardening = []string{
	// Hooks live in the clone's shared .git/hooks, which an agent can write.
	"-c", "core.hooksPath=/dev/null",
	// A program git would otherwise run on almost every command.
	"-c", "core.fsmonitor=false",
	// Signing runs gpg.program. Onett's commits are unsigned.
	"-c", "commit.gpgSign=false",
	// Never let git start git inside a submodule: that git reads the submodule's own config.
	"-c", "submodule.recurse=false",
	"-c", "fetch.recurseSubmodules=false",
	"-c", "push.recurseSubmodules=no",
	// The ext:: transport runs a command named in a URL.
	"-c", "protocol.ext.allow=never",
	// A commit-graph file is a cache in the object store; a forged one can lie about a commit's parents.
	"-c", "core.commitGraph=false",
}

// gitCommand builds a git command that needs no network, run in the checkout at dir with
// hardening in front of args. Before the command exists, the checkout's .git is checked
// (resolveLayout) and its config is reduced to what onett accepts (repairConfig). The command
// gets localEnv, which holds no credentials, and git's directories are pinned through the
// environment, so replacing the checkout's .git afterwards cannot send git somewhere else.
func gitCommand(ctx context.Context, dir string, args ...string) (*exec.Cmd, error) {
	l, err := resolveLayout(dir)
	if err != nil {
		return nil, err
	}
	if err := repairConfig(l.commonDir, ""); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, hardening...), args...)...)
	cmd.Dir = dir
	cmd.Env = append(localEnv(), "GIT_DIR="+l.gitDir, "GIT_COMMON_DIR="+l.commonDir,
		"GIT_WORK_TREE="+l.workTree)
	return cmd, nil
}

// netCommand builds a git command that runs with onett's whole environment, credentials
// included. It must only run where no agent has written anything: inside a scratch repository
// (see scratch), or to make a clone that does not exist yet. dir may be empty.
func netCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, hardening...), args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return cmd
}

// runGitCmd runs a command built by gitCommand or netCommand and returns its trimmed stdout. A
// failure is reported with the arguments and git's stderr.
func runGitCmd(cmd *exec.Cmd, args []string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err,
			strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// git runs one git command that needs no network in the checkout at dir (see gitCommand) and
// returns its trimmed stdout. A failure is reported with the arguments and git's stderr.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd, err := gitCommand(ctx, dir, args...)
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return runGitCmd(cmd, args)
}

// gitNet runs one git command with onett's credentials; see netCommand.
func gitNet(ctx context.Context, dir string, args ...string) (string, error) {
	return runGitCmd(netCommand(ctx, dir, args...), args)
}

// gitExitCode runs git in dir and returns its exit status without treating a non-zero status as
// an error. It is used for commands such as `git diff --quiet`, where exit 1 is an answer.
func gitExitCode(ctx context.Context, dir string, args ...string) (int, error) {
	cmd, err := gitCommand(ctx, dir, args...)
	if err != nil {
		return 0, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 0, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err,
		strings.TrimSpace(stderr.String()))
}

// gitOutputAllowExit runs git in dir and returns its trimmed stdout even when git exits non-zero.
// It is used for commands that print their findings and signal them with the exit status, such
// as `git diff --check`. Only a failure to run git at all is an error.
func gitOutputAllowExit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd, err := gitCommand(ctx, dir, args...)
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err,
				strings.TrimSpace(stderr.String()))
		}
	}
	return strings.TrimSpace(stdout.String()), nil
}

// localEnvKeys is what a git command that needs no network gets from onett's environment:
// enough to find git, the owner's global config and the commit identity, and nothing that is a
// credential. If an agent manages to make such a command run a program, the program finds no
// token in its environment.
var localEnvKeys = []string{
	"PATH", "HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "LC_CTYPE", "TMPDIR", "TZ",
	"XDG_CONFIG_HOME", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM",
	"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL",
	"GIT_ALLOW_PROTOCOL",
}

// localEnv returns the environment for a git command that needs no network; see localEnvKeys.
// Replace refs and a grafts file are ways to make git show different content or history for the
// same commit, and an agent can write both, so they are switched off here.
func localEnv() []string {
	var env []string
	for _, k := range localEnvKeys {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return append(env, "GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1", "GIT_GRAFT_FILE="+os.DevNull)
}

// layout says where git keeps a checkout's files. gitDir holds HEAD and the index; commonDir
// holds config, objects and refs. They are the same directory for a clone and differ for a
// linked worktree, whose .git is a file naming <commonDir>/worktrees/<name>.
type layout struct {
	workTree, gitDir, commonDir string
}

// errNotCheckout is returned when a directory's .git is missing or does not have the shape
// onett expects, for example because an agent replaced it.
var errNotCheckout = errors.New("not a git checkout onett can use")

func notCheckout(dir, why string) error {
	return fmt.Errorf("%w: %s: %s", errNotCheckout, dir, why)
}

// resolveLayout reads dir's .git and checks that a linked worktree's .git file points at a
// worktree directory which points back at dir. Lstat is used so a symlinked .git is refused.
func resolveLayout(dir string) (layout, error) {
	dot := filepath.Join(dir, ".git")
	fi, err := os.Lstat(dot)
	if err != nil {
		return layout{}, fmt.Errorf("%w: %s: %v", errNotCheckout, dir, err)
	}
	if fi.IsDir() {
		return layout{workTree: dir, gitDir: dot, commonDir: dot}, nil
	}
	if !fi.Mode().IsRegular() {
		return layout{}, notCheckout(dir, ".git is neither a directory nor a file")
	}
	content, err := os.ReadFile(dot)
	if err != nil {
		return layout{}, fmt.Errorf("%w: %s: %v", errNotCheckout, dir, err)
	}
	s := strings.TrimSpace(string(content))
	if !strings.HasPrefix(s, "gitdir: ") {
		return layout{}, notCheckout(dir, "its .git file does not name a git directory")
	}
	p := strings.TrimPrefix(s, "gitdir: ")
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	gitDir := filepath.Clean(p)
	if filepath.Base(filepath.Dir(gitDir)) != "worktrees" {
		return layout{}, notCheckout(dir, "its .git file does not name a worktree's git directory")
	}
	commonDir := filepath.Dir(filepath.Dir(gitDir))
	back, err := os.ReadFile(filepath.Join(gitDir, "gitdir"))
	if err != nil {
		return layout{}, fmt.Errorf("%w: %s: %v", errNotCheckout, dir, err)
	}
	b := strings.TrimSpace(string(back))
	if !filepath.IsAbs(b) {
		b = filepath.Join(gitDir, b)
	}
	if !samePath(b, dot) {
		return layout{}, notCheckout(dir, "its git directory belongs to another worktree")
	}
	return layout{workTree: dir, gitDir: gitDir, commonDir: commonDir}, nil
}

// requireWorktree fails unless dir is a linked worktree whose .git file is intact.
func requireWorktree(dir string) error {
	l, err := resolveLayout(dir)
	if err != nil {
		return err
	}
	if l.gitDir == l.commonDir {
		return notCheckout(dir, "its .git was replaced by a directory, not a worktree's file")
	}
	return nil
}

// checkWorktreeOf fails unless dir is a linked worktree of the clone at clonePath.
func checkWorktreeOf(clonePath, dir string) error {
	if err := requireWorktree(dir); err != nil {
		return err
	}
	c, err := resolveLayout(clonePath)
	if err != nil {
		return err
	}
	l, err := resolveLayout(dir)
	if err != nil {
		return err
	}
	if !sameDir(c.commonDir, l.commonDir) {
		return notCheckout(dir, "the worktree does not belong to the clone at "+clonePath)
	}
	return nil
}

// samePath reports whether a and b name the same file. The parent directory is resolved through
// symlinks, because macOS temp directories are symlinks and a plain comparison would miss a match.
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := resolvePath(a)
	rb, errB := resolvePath(b)
	return errA == nil && errB == nil && ra == rb
}

func resolvePath(p string) (string, error) {
	dir, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.Base(p)), nil
}

// sameDir reports whether a and b are the same directory, after resolving symlinks.
func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

var (
	configSectionRe = regexp.MustCompile(`^\[([A-Za-z][A-Za-z0-9-]*)(?: "([^"\\]*)")?\]$`)
	configKeyRe     = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9-]*)\s*=\s*(.*)$`)
	configQuotedRe  = regexp.MustCompile(`^"((?:[^"\\]|\\["\\])*)"$`)
	configRemoteRe  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	configFetchRe   = regexp.MustCompile(`^\+?[A-Za-z0-9*/._-]+:[A-Za-z0-9*/._-]+$`)
	configMergeRe   = regexp.MustCompile(`^refs/[A-Za-z0-9._/@+-]+$`)
	configUnescape  = strings.NewReplacer(`\"`, `"`, `\\`, `\`)
	configEscape    = strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	// configGroupKeys is the order in which a section's settings are written.
	configGroupKeys = map[string][]string{
		"remote": {"url"},
		"branch": {"remote", "merge"},
		"user":   {"name", "email"},
	}
)

// configGroup is one [section "subsection"] that onett keeps.
type configGroup struct {
	sec, sub string
	vals     map[string]string
	fetch    []string
}

// cleanText reports whether s is valid UTF-8 with no control characters.
func cleanText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// isBool reports whether v is one of the spellings git accepts for a boolean.
func isBool(v string) bool {
	switch strings.ToLower(v) {
	case "true", "false", "yes", "no", "on", "off", "1", "0":
		return true
	}
	return false
}

// allowedSetting reports whether onett keeps a setting, given its value. Anything not listed here
// is dropped, so a new git setting that can run a program is refused until someone adds it.
func allowedSetting(sec, sub, key, value string) bool {
	switch sec {
	case "core":
		if sub != "" {
			return false
		}
		switch key {
		case "repositoryformatversion":
			return value == "0" || value == "1"
		case "filemode", "ignorecase", "precomposeunicode", "symlinks":
			return isBool(value)
		case "logallrefupdates":
			return isBool(value) || value == "always"
		}
	case "extensions":
		if sub != "" {
			return false
		}
		switch key {
		case "objectformat":
			return value == "sha1" || value == "sha256"
		case "refstorage":
			return value == "files" || value == "reftable"
		case "relativeworktrees":
			return isBool(value)
		}
	case "remote":
		if !configRemoteRe.MatchString(sub) {
			return false
		}
		switch key {
		case "url":
			return !strings.HasPrefix(value, "-") && !strings.Contains(value, "::")
		case "fetch":
			return configFetchRe.MatchString(value)
		}
	case "branch":
		if sub == "" {
			return false
		}
		switch key {
		case "remote":
			return value == "." || (configRemoteRe.MatchString(value) && !strings.HasPrefix(value, "-"))
		case "merge":
			return configMergeRe.MatchString(value)
		}
	case "user":
		return sub == "" && (key == "name" || key == "email")
	}
	return false
}

// canonicalConfig returns the config text onett accepts for a repository. It is written fresh
// from the settings in existing that pass allowedSetting; no input byte is copied except inside a
// value that passed its check, and lines the parser does not understand are dropped. originURL,
// when not empty, becomes remote.origin.url.
func canonicalConfig(existing []byte, originURL string) ([]byte, error) {
	core := map[string]string{}
	ext := map[string]string{}
	var groups []*configGroup
	group := func(sec, sub string) *configGroup {
		for _, g := range groups {
			if g.sec == sec && g.sub == sub {
				return g
			}
		}
		g := &configGroup{sec: sec, sub: sub, vals: map[string]string{}}
		groups = append(groups, g)
		return g
	}

	var sec, sub string
	valid := false
	for _, line := range strings.Split(string(existing), "\n") {
		t := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if t == "" || t[0] == '#' || t[0] == ';' {
			continue
		}
		if t[0] == '[' {
			// A header that does not parse makes the keys under it unusable, so they are not
			// counted under the previous section.
			m := configSectionRe.FindStringSubmatch(t)
			valid = false
			if m == nil || (m[2] != "" && !cleanText(m[2])) {
				continue
			}
			sec, sub, valid = strings.ToLower(m[1]), m[2], true
			continue
		}
		if !valid {
			continue
		}
		m := configKeyRe.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		key, raw := strings.ToLower(m[1]), m[2]
		var value string
		if strings.HasPrefix(raw, `"`) {
			q := configQuotedRe.FindStringSubmatch(raw)
			if q == nil {
				continue
			}
			value = configUnescape.Replace(q[1])
		} else {
			// An unquoted value with a quote, backslash, comment mark or line continuation
			// would be read differently by git than by this parser, so it is dropped.
			if strings.ContainsAny(raw, `"\#;`) {
				continue
			}
			value = raw
		}
		if value == "" || !cleanText(value) || !allowedSetting(sec, sub, key, value) {
			continue
		}
		switch {
		case sec == "core":
			core[key] = value
		case sec == "extensions":
			ext[key] = value
		case key == "fetch":
			g := group(sec, sub)
			if !slices.Contains(g.fetch, value) {
				g.fetch = append(g.fetch, value)
			}
		default:
			group(sec, sub).vals[key] = value
		}
	}

	if originURL != "" {
		if !cleanText(originURL) {
			return nil, errors.New("origin URL has a control character")
		}
		g := group("remote", "origin")
		g.vals["url"] = originURL
		if len(g.fetch) == 0 {
			g.fetch = []string{"+refs/heads/*:refs/remotes/origin/*"}
		}
	}

	var b strings.Builder
	b.WriteString("[core]\n")
	for _, k := range []string{"repositoryformatversion", "filemode", "bare", "logallrefupdates",
		"ignorecase", "precomposeunicode", "symlinks"} {
		if k == "bare" {
			b.WriteString("\tbare = false\n")
		} else if v, ok := core[k]; ok {
			writeConfigLine(&b, k, v)
		}
	}
	if len(ext) > 0 {
		b.WriteString("[extensions]\n")
		for _, k := range []string{"objectformat", "refstorage", "relativeworktrees"} {
			if v, ok := ext[k]; ok {
				writeConfigLine(&b, k, v)
			}
		}
	}
	for _, g := range groups {
		if len(g.vals) == 0 && len(g.fetch) == 0 {
			continue
		}
		if g.sec == "user" {
			b.WriteString("[user]\n")
		} else {
			b.WriteString("[" + g.sec + " \"" + g.sub + "\"]\n")
		}
		for _, k := range configGroupKeys[g.sec] {
			if v, ok := g.vals[k]; ok {
				writeConfigLine(&b, k, v)
			}
		}
		for _, f := range g.fetch {
			writeConfigLine(&b, "fetch", f)
		}
	}
	return []byte(b.String()), nil
}

// writeConfigLine writes one "key = value" line in the form canonicalConfig produces.
func writeConfigLine(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, "\t%s = %s\n", key, quoteValue(value))
}

// quoteValue quotes a value only when git would otherwise trim it or read a special character.
func quoteValue(v string) string {
	if strings.HasPrefix(v, " ") || strings.HasPrefix(v, "\t") || strings.HasSuffix(v, " ") ||
		strings.HasSuffix(v, "\t") || strings.ContainsAny(v, `"\#;`) {
		return `"` + configEscape.Replace(v) + `"`
	}
	return v
}

// maxConfigBytes is the largest config file onett reads. A real one is a few hundred bytes.
const maxConfigBytes = 1 << 20

// readConfigFile returns the content of a config file an agent may have replaced. It does not
// follow a symlink (the agent could point it at a file it cannot read itself), does not block on
// a FIFO, and gives up on anything that is not a regular file of a sane size. In those cases it
// returns no content, and the caller writes a fresh file in its place.
func readConfigFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	// Opening a symlink with O_NOFOLLOW fails with ELOOP (EMLINK on some systems).
	if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EMLINK) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() || fi.Size() > maxConfigBytes {
		return nil, nil
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxConfigBytes {
		return nil, nil
	}
	return data, nil
}

// repairConfig rewrites <commonDir>/config to canonicalConfig of its current content when the
// two differ. The current content is read by readConfigFile, so a symlink, a FIFO or a huge file
// is never read; the new file is written beside the old and renamed over it, so the old one is
// replaced, not followed. No copy of the old content is kept.
func repairConfig(commonDir, originURL string) error {
	path := filepath.Join(commonDir, "config")
	existing, err := readConfigFile(path)
	if err != nil {
		return err
	}
	want, err := canonicalConfig(existing, originURL)
	if err != nil {
		return err
	}
	if existing != nil && bytes.Equal(existing, want) {
		return nil
	}
	slog.Warn("onett rewrote a repository's git config to the settings it accepts", "config", path)

	// Write to a temp file and rename, so git never reads a half-written config.
	tmp, err := os.CreateTemp(commonDir, "config.onett-*")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name) // a no-op once the file has been renamed into place
	_, err = tmp.Write(want)
	if err == nil {
		err = tmp.Chmod(0o644)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// scratch is a repository onett creates and deletes after one use. It is made under onett's own
// directory, not the system temp directory, because an agent's sandbox may allow writes to the
// temp directory. It is the only place git runs with onett's credentials: no agent has written
// its config, so nothing in it can name a program to run or another host to send a token to. It
// borrows the clone's objects (objects/info/alternates), so a push copies nothing and a fetch
// downloads only what is new.
type scratch struct {
	dir string
}

// newScratch makes a scratch repository that borrows the objects of the clone at clonePath. It is
// made in parent, or in the system temp directory when parent is empty.
func newScratch(ctx context.Context, parent, clonePath string) (*scratch, error) {
	format, err := git(ctx, clonePath, "rev-parse", "--show-object-format")
	if err != nil {
		return nil, err
	}
	l, err := resolveLayout(clonePath)
	if err != nil {
		return nil, err
	}
	objects, err := filepath.Abs(filepath.Join(l.commonDir, "objects"))
	if err != nil {
		return nil, err
	}
	var dir string
	if parent != "" {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return nil, fmt.Errorf("create scratch directory: %w", err)
		}
		dir, err = os.MkdirTemp(parent, "onett-git-")
	} else {
		dir, err = os.MkdirTemp("", "onett-git-")
	}
	if err != nil {
		return nil, err
	}
	if _, err := gitNet(ctx, dir, "init", "--quiet", "--object-format="+format); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	infoDir := filepath.Join(dir, ".git", "objects", "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(infoDir, "alternates"), []byte(objects+"\n"), 0o644); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return &scratch{dir: dir}, nil
}

// close deletes the scratch repository.
func (s *scratch) close() {
	os.RemoveAll(s.dir)
}

// git runs a command in the scratch repository without credentials.
func (s *scratch) git(ctx context.Context, args ...string) (string, error) {
	return git(ctx, s.dir, args...)
}

// gitNet runs a command in the scratch repository with credentials. With borrowed objects, a
// fetch would otherwise start `git for-each-ref` inside the clone, with credentials in its
// environment, reading the clone's config. `true` prints nothing, so no git with credentials ever
// runs in the clone. The command is pinned to the scratch's own .git, like every other command,
// so a replaced .git in the scratch directory cannot redirect it.
func (s *scratch) gitNet(ctx context.Context, args ...string) (string, error) {
	cmd := netCommand(ctx, s.dir, append([]string{"-c", "core.alternateRefsCommand=true"}, args...)...)
	cmd.Env = append(cmd.Env, "GIT_DIR="+filepath.Join(s.dir, ".git"), "GIT_WORK_TREE="+s.dir)
	return runGitCmd(cmd, args)
}
