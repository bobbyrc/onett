package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bobbyrc/onett/internal/client"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/onboard"
	"github.com/bobbyrc/onett/internal/workspace"
)

// onboarder is what the add flow needs from onboard.Service; tests replace newOnboarder.
type onboarder interface {
	Inspect(ctx context.Context, req onboard.Request, progress func(onboard.Step)) (onboard.Result, error)
	Discard(name string, existing map[string]string) error
	Claim(dataDir, repo, name string) error
	Repos(ctx context.Context, existing map[string]string) onboard.ReposResult
}

var newOnboarder = func(cfg *config.Config) onboarder {
	return onboard.New(workspace.New(filepath.Join(cfg.Server.DataDir, "worktrees")), nil)
}

// reloadDaemon asks a running daemon to re-read the config.
var reloadDaemon = func(cfg *config.Config) error {
	token, err := apiToken(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return client.New(apiBaseURL(cfg), token).ReloadConfig(ctx)
}

const projectAddUsage = "project add [REPO] [--name N] [--branch B] [--policy supervised|default] " +
	"[--use-guess | --setup CMD... | --check CMD... | --no-checks] [--yes] [--config P]"

// errNothingAdded ends a run that wrote nothing. "Nothing was added." is printed before it.
var errNothingAdded = errors.New("nothing was added")

// addOptions is what the add flow takes from the command line, or from `onett init`.
type addOptions struct {
	repo, name, branch, policy string
	useGuess, noChecks, yes    bool
	setup, checks              []string
}

// addProject adds a project to the config: `onett project add [REPO]`. The older form,
// `onett project add NAME --repo REPO`, still works. It runs without the daemon; the daemon is
// asked to reload afterwards.
func addProject(e *env, args []string) error {
	fs := triggerFlags(e, "project add", "usage: onett "+projectAddUsage)
	cfgFlag := fs.String("config", "", "config file to edit (default $ONETT_CONFIG or ~/.config/onett/onett.yaml)")
	name := fs.String("name", "", "the project's name (default: the repository's name)")
	repo := fs.String("repo", "", "the repository, owner/name (the older form: project add NAME --repo REPO)")
	branch := fs.String("branch", "", "the branch agents base their work on (default: the repository's default branch)")
	policy := fs.String("policy", "supervised", "supervised (small, low-risk changes merge on their own) or default (every change does)")
	useGuess := fs.Bool("use-guess", false, "use the setup and checks onett guessed from the repository's files")
	noChecks := fs.Bool("no-checks", false, "no setup and no checks")
	yes := fs.Bool("yes", false, "do not ask to confirm adding the project")
	var setup, checks multiFlag
	fs.Var(&setup, "setup", "a setup command that runs before each task (repeatable)")
	fs.Var(&checks, "check", "a check command that must pass before a change merges (repeatable)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	opts := addOptions{branch: *branch, policy: *policy, useGuess: *useGuess, noChecks: *noChecks,
		yes: *yes, setup: setup, checks: checks}
	switch {
	case *repo != "":
		if len(positional) > 1 {
			return usagef("usage: onett %s", projectAddUsage)
		}
		if len(positional) == 1 {
			if *name != "" {
				return usagef("give the project's name with --name or as NAME, not both")
			}
			*name = positional[0]
		}
		opts.repo = *repo
	case len(positional) > 1:
		return usagef("usage: onett %s", projectAddUsage)
	case len(positional) == 1:
		opts.repo = positional[0]
	}
	opts.name = *name
	if opts.repo != "" {
		if _, err := onboard.NormalizeRepo(opts.repo); err != nil {
			return usagef("%v", err)
		}
	}
	path, err := configPath(*cfgFlag)
	if err != nil {
		return err
	}
	return addFlow(e, path, opts)
}

// checkAddFlags rejects flag combinations that cannot mean one thing.
func checkAddFlags(o addOptions) error {
	if o.name != "" && !projectName.MatchString(o.name) {
		return usagef("project name %q may use letters, digits, '.', '_' and '-'", o.name)
	}
	hasCommands := len(o.setup) > 0 || len(o.checks) > 0
	switch {
	case o.noChecks && (o.useGuess || hasCommands):
		return usagef("--no-checks cannot be combined with --use-guess, --setup or --check")
	case o.useGuess && hasCommands:
		return usagef("--use-guess cannot be combined with --setup or --check")
	}
	return nil
}

// addFlow runs the questions of `project add` in order and writes the project only after the
// last answer. Without a terminal it never asks: whatever it would have asked must be given as
// a flag, or it stops with "Nothing was added.".
func addFlow(e *env, cfgPath string, o addOptions) error {
	if err := checkAddFlags(o); err != nil {
		return err
	}
	interactive := e.isTTY && e.in != nil
	if o.repo == "" && !interactive {
		return usagef("a repository is required: onett project add OWNER/NAME")
	}
	data, err := os.ReadFile(cfgPath)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no config at %s; run \"onett init\" first", cfgPath)
	} else if err != nil {
		return err
	}
	cfg, err := config.Parse(data, cfgPath)
	if err != nil {
		return err
	}
	if o.policy == "" {
		o.policy = "supervised"
	}
	if _, ok := cfg.Policies[o.policy]; !ok {
		return usagef("--policy %q: no such policy in %s", o.policy, cfgPath)
	}
	if o.name != "" {
		if _, ok := cfg.Projects[o.name]; ok {
			return fmt.Errorf("project %q already exists in %s", o.name, cfgPath)
		}
	}
	existing := make(map[string]string, len(cfg.Projects))
	for n, p := range cfg.Projects {
		existing[n] = p.Repo
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ob := newOnboarder(cfg)
	in := bufio.NewReader(e.in)
	ask := func(question string) string {
		fmt.Fprint(e.out, question)
		line, _ := in.ReadString('\n')
		return strings.TrimSpace(line)
	}

	repo := o.repo
	if repo == "" {
		if repo, err = chooseRepo(e, ctx, ob, existing, ask); err != nil {
			return err
		}
		if repo == "" {
			fmt.Fprintln(e.out, "Nothing was added.")
			return nil
		}
		if _, err := onboard.NormalizeRepo(repo); err != nil {
			return usagef("%v", err)
		}
	}

	fmt.Fprintf(e.out, "Checking %s\n", repo)
	dataDir := cfg.Server.DataDir
	copyName := onboard.NameFor(repo)
	var late []onboard.Step
	progress := func(s onboard.Step) {
		switch s.Name {
		case "copy":
			printCopyStep(e, s, dataDir, copyName)
		case "branch", "pull_requests":
			late = append(late, s)
		}
	}
	res, err := ob.Inspect(ctx, onboard.Request{Repo: repo, DataDir: dataDir, Existing: existing}, progress)
	if err != nil {
		return err
	}
	// The branch and pull request lines name what the result says (GitHub or the repository,
	// the signed-in user), so they are printed once the check is over, in the same order.
	for _, s := range late {
		printLateStep(e, s, res)
	}
	if !res.Ready() {
		if res.Suggest != "" {
			fmt.Fprintf(e.out, "        Did you mean %s?\n", res.Suggest)
		}
		discardCopy(e, ob, res.Name, existing)
		fmt.Fprintln(e.out, "Nothing was added.")
		return errNothingAdded
	}

	setup, checks, ok := chooseCommands(e, ask, interactive, o, res)
	if !ok {
		discardCopy(e, ob, res.Name, existing)
		fmt.Fprintln(e.out, "Nothing was added.")
		return errNothingAdded
	}
	fmt.Fprintln(e.out, policySentence(o.policy))

	name := res.Name
	if o.name != "" {
		name = o.name
	}
	if name != res.Name {
		if dest := filepath.Join(dataDir, "repos", name); pathExists(dest) {
			discardCopy(e, ob, res.Name, existing)
			fmt.Fprintln(e.out, "Nothing was added.")
			return fmt.Errorf("%s already exists", dest)
		}
	}
	if interactive && !o.yes {
		if answer := ask(fmt.Sprintf("\nAdd %s? [Y/n]: ", name)); strings.EqualFold(answer, "n") || strings.EqualFold(answer, "no") {
			discardCopy(e, ob, res.Name, existing)
			fmt.Fprintln(e.out, "Nothing was added.")
			return nil
		}
	}

	branch := o.branch
	if branch == "" {
		branch = res.DefaultBranch
	}
	out, err := config.AddProject(data, cfgPath, config.NewProject{
		Name: name, Repo: res.Repo, Branch: branch, Policy: o.policy, Setup: setup, Checks: checks,
	})
	if err != nil {
		discardCopy(e, ob, res.Name, existing)
		return err
	}
	if err := config.WriteFileAtomic(cfgPath, out); err != nil {
		discardCopy(e, ob, res.Name, existing)
		return err
	}
	if err := ob.Claim(dataDir, res.Repo, name); err != nil {
		fmt.Fprintf(e.errOut, "onett: warning: could not record onett's copy of %s: %v\n", name, err)
	}

	if err := reloadDaemon(cfg); err != nil {
		fmt.Fprintf(e.out, "Added %s to %s.\nonett is not running; it will read the new project when it starts.\n", name, cfgPath)
	} else {
		fmt.Fprintf(e.out, "Added %s to %s and reloaded onett.\n", name, cfgPath)
	}
	fmt.Fprintf(e.out, "See it work once (a question task asks the agent to read the code and change nothing):\n"+
		"  onett task create --project %s --kind question --title \"Explain how %s is built and tested\" "+
		"--body \"Read the repository and explain how it is built, tested and laid out. Do not change any files.\"\n", name, name)
	return nil
}

// chooseRepo lists the owner's repositories and asks which one, or, when the GitHub CLI is not
// signed in, asks for the repository by name. It returns "" for a blank answer.
func chooseRepo(e *env, ctx context.Context, ob onboarder, existing map[string]string, ask func(string) string) (string, error) {
	rr := ob.Repos(ctx, existing)
	if !rr.GH.SignedIn {
		fmt.Fprint(e.out, "The GitHub CLI is not signed in on this machine. onett uses it to list your repositories\n"+
			"and to open pull requests. Run:\n  gh auth login\n")
		return ask("Repository (owner/name, a GitHub link, or a local path): "), nil
	}
	if rr.GH.Error != "" {
		fmt.Fprintf(e.out, "Could not list your repositories: %s\n", oneLine(rr.GH.Error))
	}
	fmt.Fprintf(e.out, "Your repositories, most recently pushed first (GitHub CLI signed in as %s):\n", oneLine(rr.GH.User))
	var shown []onboard.GitHubRepo
	now := time.Now()
	for _, r := range rr.Repos {
		if r.Project != nil || len(shown) == 10 {
			continue
		}
		shown = append(shown, r)
		visibility := "public"
		if r.Private {
			visibility = "private"
		}
		fmt.Fprintf(e.out, "  %3d  %-40s %-8s %s\n", len(shown), oneLine(r.FullName), visibility, ageWords(now, r.PushedAt))
	}
	answer := ask("Repository (a number, owner/name, or a GitHub link): ")
	n, err := strconv.Atoi(answer)
	if err != nil {
		return answer, nil
	}
	if n < 1 || n > len(shown) {
		return "", fmt.Errorf("no repository numbered %d in the list", n)
	}
	return shown[n-1].FullName, nil
}

// ageWords says how long ago a repository was last pushed, in a few words.
func ageWords(now, t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	day := 24 * time.Hour
	switch {
	case d < time.Hour:
		return "just now"
	case d < day:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d < 2*day:
		return "yesterday"
	case d < 7*day:
		return fmt.Sprintf("%dd ago", int(d/day))
	case d < 30*day:
		return fmt.Sprintf("%dw ago", int(d/(7*day)))
	case d < 365*day:
		return fmt.Sprintf("%dmo ago", int(d/(30*day)))
	}
	return fmt.Sprintf("%dy ago", int(d/(365*day)))
}

// printCopyStep prints the copy step as it finishes: where onett's own copy is, or why it failed.
func printCopyStep(e *env, s onboard.Step, dataDir, name string) {
	switch s.State {
	case "done":
		fmt.Fprintf(e.out, "  done  copied   onett's own copy, in %s; keep working in yours\n",
			tildePath(filepath.Join(dataDir, "repos", name)))
	case "failed":
		fmt.Fprintln(e.out, "  STOP  could not copy the repository:")
		for _, line := range strings.Split(strings.TrimSpace(cleanText(s.Detail)), "\n") {
			fmt.Fprintf(e.out, "          %s\n", line)
		}
	}
}

// printLateStep prints the branch and pull request steps, once the check's result is known.
func printLateStep(e *env, s onboard.Step, res onboard.Result) {
	switch {
	case s.Name == "branch" && s.State == "done":
		from := "the repository"
		if res.BranchFrom == "github" {
			from = "GitHub"
		}
		fmt.Fprintf(e.out, "  done  branch   %s, from %s\n", oneLine(res.DefaultBranch), from)
	case s.Name == "branch" && s.State == "failed":
		fmt.Fprintf(e.out, "  STOP  could not read the default branch: %s\n", oneLine(s.Detail))
	case s.Name == "pull_requests" && s.State == "done":
		fmt.Fprintf(e.out, "  done  pull requests   GitHub CLI signed in as %s\n", oneLine(res.PullRequests.GHUser))
	case s.Name == "pull_requests" && s.State == "skipped" && res.Copy.OK && !res.GitHub:
		fmt.Fprintln(e.out, "  skip  pull requests   not a GitHub repository: onett cannot open pull requests for it")
	case s.Name == "pull_requests" && s.State == "failed":
		fmt.Fprintln(e.out, "  STOP  pull requests   the GitHub CLI is not signed in on this machine. Run gh auth login, then try again.")
	}
}

// chooseCommands settles the setup and checks: from the flags, from the guess, or from the
// person's answers. ok is false when there is nothing to offer without a terminal.
func chooseCommands(e *env, ask func(string) string, interactive bool, o addOptions, res onboard.Result) (setup, checks []config.Check, ok bool) {
	g := res.Guess
	switch {
	case len(o.setup) > 0 || len(o.checks) > 0:
		setupCmds, checkCmds := checksFrom(o.setup), checksFrom(o.checks)
		writeCommandBlock(e.out, "\nBefore every task (from --setup and --check):", checkRuns(setupCmds), checkRuns(checkCmds), "")
		return setupCmds, checkCmds, true
	case o.noChecks:
		fmt.Fprintln(e.out, "\nNo setup or checks (--no-checks): agents get the code with nothing installed, and onett opens pull requests without testing them first.")
		return nil, nil, true
	case o.useGuess:
		if len(g.Setup)+len(g.Checks) == 0 {
			fmt.Fprintln(e.out, "\nBefore every task: onett found nothing it knows how to install or test.\nNothing to use (--use-guess).")
			return nil, nil, true
		}
		writeGuessBlock(e.out, g, "Using them (--use-guess).")
		return commandsFrom(g.Setup), commandsFrom(g.Checks), true
	case len(g.Setup)+len(g.Checks) > 0:
		return offerGuess(e, ask, interactive, res)
	}
	fmt.Fprintf(e.out, "\nBefore every task: onett found nothing it knows how to install or test.\nAt the top of %s: %s\n",
		res.Name, strings.Join(g.TopFiles, "  "))
	if !interactive {
		fmt.Fprintln(e.out, "Pass --setup CMD / --check CMD, or --no-checks for none.")
		return nil, nil, false
	}
	var setupRuns, checkRuns []string
	if s := ask("Setup command, empty for none: "); s != "" {
		setupRuns = append(setupRuns, s)
	}
	for c := ask("Check command, empty when done: "); c != ""; c = ask("Check command, empty when done: ") {
		checkRuns = append(checkRuns, c)
	}
	return checksFrom(setupRuns), checksFrom(checkRuns), true
}

// offerGuess shows the guessed setup and checks and asks to use them, edit them or drop them.
// Without a terminal it stops and names the flags that would accept them.
func offerGuess(e *env, ask func(string) string, interactive bool, res onboard.Result) (setup, checks []config.Check, ok bool) {
	g := res.Guess
	if !interactive {
		var parts []string
		if len(g.Setup) > 0 {
			parts = append(parts, "setup "+joinRuns(g.Setup))
		}
		if len(g.Checks) > 0 {
			parts = append(parts, "checks "+joinRuns(g.Checks))
		}
		fmt.Fprintf(e.out, "onett guessed setup and checks for %s:\n  %s\n"+
			"Pass --use-guess to accept them, --setup CMD / --check CMD to give your own,\nor --no-checks for none.\n",
			res.Name, strings.Join(parts, " · "))
		return nil, nil, false
	}
	writeGuessBlock(e.out, g, "")

	for {
		answer := ask("Use these? [Y]es, [e]dit, [n]o setup or checks: ")
		switch strings.ToLower(answer) {
		case "", "y", "yes":
			return commandsFrom(g.Setup), commandsFrom(g.Checks), true
		case "n", "no":
			return nil, nil, true
		case "e", "edit":
			setupRuns := editCommands(ask, "setup", g.Setup)
			checkRuns := editCommands(ask, "check", g.Checks)
			for s := ask("Another setup command, empty when done: "); s != ""; s = ask("Another setup command, empty when done: ") {
				setupRuns = append(setupRuns, s)
			}
			for c := ask("Another check command, empty when done: "); c != ""; c = ask("Another check command, empty when done: ") {
				checkRuns = append(checkRuns, c)
			}
			return checksFrom(setupRuns), checksFrom(checkRuns), true
		}
	}
}

// editCommands asks about each guessed command: Enter keeps it, - removes it, anything else
// replaces it.
func editCommands(ask func(string) string, label string, cmds []onboard.Command) []string {
	var runs []string
	for _, c := range cmds {
		answer := ask(fmt.Sprintf("%s %s (Enter keeps it, - removes it, or type a new command): ", label, c.Run))
		switch answer {
		case "":
			runs = append(runs, c.Run)
		case "-":
		default:
			runs = append(runs, answer)
		}
	}
	return runs
}

// writeCommandBlock prints a header, then the setup and checks lines, in the layout every
// block uses. footer, when not empty, is printed last.
func writeCommandBlock(w io.Writer, header string, setup, checks []string, footer string) {
	fmt.Fprintln(w, header)
	writeCommandLines(w, "setup", setup)
	writeCommandLines(w, "checks", checks)
	if footer != "" {
		fmt.Fprintln(w, footer)
	}
}

// writeGuessBlock prints the guessed commands, named by the files they were guessed from. An
// empty footer is left out; "They run..." always follows the lines.
func writeGuessBlock(w io.Writer, g onboard.Guess, footer string) {
	var froms []string
	seen := map[string]bool{}
	for _, c := range append(append([]onboard.Command{}, g.Setup...), g.Checks...) {
		if !seen[c.From] {
			seen[c.From] = true
			froms = append(froms, c.From)
		}
	}
	setup, checks := runsOf(g.Setup), runsOf(g.Checks)
	writeCommandBlock(w, fmt.Sprintf("\nBefore every task, guessed from %s:", strings.Join(froms, " and ")),
		setup, checks, "They run in each task's copy with the agent's limited environment, never your credentials.")
	if footer != "" {
		fmt.Fprintln(w, footer)
	}
}

// writeCommandLines prints one labelled group: the label on the first line, the rest indented.
func writeCommandLines(w io.Writer, label string, runs []string) {
	for i, r := range runs {
		if i == 0 {
			fmt.Fprintf(w, "  %-8s %s\n", label, r)
		} else {
			fmt.Fprintf(w, "           %s\n", r)
		}
	}
}

// runsOf returns the command lines of guessed commands.
func runsOf(cmds []onboard.Command) []string {
	runs := make([]string, len(cmds))
	for i, c := range cmds {
		runs[i] = c.Run
	}
	return runs
}

func joinRuns(cmds []onboard.Command) string {
	return strings.Join(runsOf(cmds), ", ")
}

// policySentence says who merges under the policy the project is added with.
func policySentence(policy string) string {
	switch policy {
	case "supervised":
		return "\nWho merges: small, low-risk changes merge on their own once the reviewing agent approves\n" +
			"and CI passes; bigger ones, and changes to dependencies, CI, migrations or auth, wait for you."
	case "default":
		return "\nWho merges: every change merges on its own once the reviewing agent approves and CI passes, whatever its risk."
	}
	return "\nWho merges: policy " + policy + "."
}

// checkRuns returns the command lines of config checks.
func checkRuns(cs []config.Check) []string {
	runs := make([]string, len(cs))
	for i, c := range cs {
		runs[i] = c.Run
	}
	return runs
}

// commandsFrom turns guessed commands into config checks.
func commandsFrom(cmds []onboard.Command) []config.Check {
	runs := make([]string, len(cmds))
	for i, c := range cmds {
		runs[i] = c.Run
	}
	return checksFrom(runs)
}

// checksFrom turns command lines into config checks. A command is named by its text; blanks and
// repeats are dropped.
func checksFrom(runs []string) []config.Check {
	var out []config.Check
	seen := map[string]bool{}
	for _, r := range runs {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, config.Check{Name: r, Run: r})
	}
	return out
}

// discardCopy removes the copy this run made. A copy that is not there any more is not an error.
func discardCopy(e *env, ob onboarder, name string, existing map[string]string) {
	if err := ob.Discard(name, existing); err != nil && !errors.Is(err, onboard.ErrNotFound) {
		fmt.Fprintf(e.errOut, "onett: could not remove onett's copy of %s: %v\n", name, err)
	}
}

// tildePath shortens a path under the home directory to ~/...
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home || strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// checkInsertion exists for the tests of the guard, which lives in package config.
func checkInsertion(before, after []byte, path, name string, setsSelf bool) error {
	return config.CheckInsertion(before, after, path, name, setsSelf)
}
