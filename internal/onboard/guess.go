package onboard

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// maxSubdirs caps how many subdirectories are scanned, so a monorepo gives a short list a person
// can read.
const maxSubdirs = 5

// maxTopFiles caps the file list shown to the person.
const maxTopFiles = 12

// skipDirs are directories that hold other people's code or build output, not the project's own
// setup.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "testdata": true, "dist": true, "build": true,
}

// makeTest finds a "test:" target in a Makefile.
var makeTest = regexp.MustCompile(`(?m)^test\s*:`)

// subdirName is the subdirectory names a command may enter. The name goes into a shell `cd`
// command unquoted, so anything else is skipped.
var subdirName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// maxReadBytes caps how much of a repository's file detection reads. A larger file is unreadable.
const maxReadBytes = 1 << 20

// scanDir is one directory the guess looks at. cd, from and name are the prefixes it adds to
// each command: the directory to enter, the path shown as the file a guess came from, and the
// display name.
type scanDir struct {
	dir, cd, from, name string
}

// GuessCommands reads the files of dir and proposes setup and check commands. It only reads
// files: nothing the repository contains is run.
func GuessCommands(dir string) Guess {
	g := Guess{Setup: []Command{}, Checks: []Command{}, TopFiles: topFiles(dir)}
	mise := ""
	if anyRegular(dir, "mise.toml", ".mise.toml", ".tool-versions") {
		mise = "mise exec -- "
	}
	scans := []scanDir{{dir: dir}}
	for _, sub := range subdirs(dir) {
		scans = append(scans, scanDir{dir: filepath.Join(dir, sub), cd: "cd " + sub + " && ",
			from: sub + "/", name: sub + ": "})
	}

	// The same command can come from two directories; the first one wins.
	seen := map[string]bool{}
	keep := func(list []Command, cs []Command) []Command {
		for _, c := range cs {
			if !seen[c.Run] {
				seen[c.Run] = true
				list = append(list, c)
			}
		}
		return list
	}
	for _, sc := range scans {
		setup, checks := guessDir(sc, mise)
		g.Setup = keep(g.Setup, setup)
		g.Checks = keep(g.Checks, checks)
	}
	return g
}

// guessDir proposes the commands one directory's files call for.
func guessDir(sc scanDir, mise string) (setup, checks []Command) {
	// cmd builds a command for text, which is what the person reads; Run adds the directory and
	// tool prefixes that make it run.
	cmd := func(text, from string) Command {
		return Command{Name: sc.name + text, Run: sc.cd + mise + text, From: sc.from + from}
	}
	in := func(name string) string { return filepath.Join(sc.dir, name) }

	if regularFile(in("package.json")) {
		install, from, pm := nodeInstall(sc.dir)
		setup = append(setup, cmd(install, from))
		scripts := packageScripts(in("package.json"))
		for _, script := range []string{"lint", "typecheck", "test"} {
			if _, ok := scripts[script]; ok {
				checks = append(checks, cmd(nodeRun(pm, script), "package.json"))
			}
		}
	}
	if regularFile(in("go.mod")) {
		setup = append(setup, cmd("go mod download", "go.mod"))
		checks = append(checks, cmd("go vet ./...", "go.mod"), cmd("go test ./...", "go.mod"))
	}
	switch {
	case regularFile(in("pyproject.toml")) && regularFile(in("uv.lock")):
		setup = append(setup, cmd("uv sync", "uv.lock"))
		if marker := pytestMarker(sc.dir); marker != "" {
			checks = append(checks, cmd("uv run pytest", marker))
		}
	case regularFile(in("poetry.lock")):
		setup = append(setup, cmd("poetry install", "poetry.lock"))
		if marker := pytestMarker(sc.dir); marker != "" {
			checks = append(checks, cmd("poetry run pytest", marker))
		}
	case regularFile(in("requirements.txt")):
		setup = append(setup, cmd("pip install -r requirements.txt", "requirements.txt"))
		if marker := pytestMarker(sc.dir); marker != "" {
			checks = append(checks, cmd("python -m pytest", marker))
		}
	}
	if regularFile(in("Cargo.toml")) {
		setup = append(setup, cmd("cargo fetch", "Cargo.toml"))
		checks = append(checks, cmd("cargo test", "Cargo.toml"))
	}
	// A Makefile's test target is the fallback: it is used only when the language files gave no check.
	if len(checks) == 0 {
		for _, mk := range []string{"Makefile", "makefile", "GNUmakefile"} {
			if data, ok := readSmall(in(mk)); ok && makeTest.Match(data) {
				checks = append(checks, cmd("make test", mk))
				break
			}
		}
	}
	return setup, checks
}

// nodeInstall picks the install command from the lockfile, and returns the package manager that
// runs the scripts. The file the command came from is returned as well.
func nodeInstall(dir string) (install, from, pm string) {
	for _, l := range []struct{ file, pm, install string }{
		{"pnpm-lock.yaml", "pnpm", "pnpm install --frozen-lockfile"},
		{"yarn.lock", "yarn", "yarn install --immutable"},
		{"bun.lockb", "bun", "bun install --frozen-lockfile"},
		{"bun.lock", "bun", "bun install --frozen-lockfile"},
		{"package-lock.json", "npm", "npm ci"},
	} {
		if regularFile(filepath.Join(dir, l.file)) {
			return l.install, l.file, l.pm
		}
	}
	return "npm install", "package.json", "npm"
}

// nodeRun is the command that runs a package.json script with the given package manager.
func nodeRun(pm, script string) string {
	switch pm {
	case "npm":
		if script == "test" {
			return "npm test"
		}
		return "npm run " + script
	case "pnpm":
		if script == "test" {
			return "pnpm test"
		}
		return "pnpm run " + script
	case "yarn":
		return "yarn " + script
	default:
		return "bun run " + script
	}
}

// packageScripts returns the keys of package.json's "scripts" object. A file that is not valid
// JSON has no scripts.
func packageScripts(path string) map[string]json.RawMessage {
	data, ok := readSmall(path)
	if !ok {
		return nil
	}
	var doc struct {
		Scripts map[string]json.RawMessage `json:"scripts"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	return doc.Scripts
}

// pytestMarker names the file or directory that shows the project has tests, or "" when it has none.
func pytestMarker(dir string) string {
	// tests/ is a directory; the other markers are files. Both are looked up without following links.
	if dirExists(filepath.Join(dir, "tests")) {
		return "tests/"
	}
	for _, m := range []string{"pytest.ini", "conftest.py"} {
		if regularFile(filepath.Join(dir, m)) {
			return m
		}
	}
	return ""
}

// subdirs returns up to maxSubdirs immediate subdirectories of dir, in name order, after the
// hidden and skipped ones are left out.
func subdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		// DirEntry reports a symlink as a symlink, not a directory, so linked directories are not entered.
		if !e.IsDir() || strings.HasPrefix(n, ".") || skipDirs[n] || !subdirName.MatchString(n) {
			continue
		}
		out = append(out, n)
		if len(out) == maxSubdirs {
			break
		}
	}
	return out
}

// topFiles lists the names in dir, sorted, with directories marked by a trailing "/". Dot files
// are left out.
func topFiles(dir string) []string {
	out := []string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".") {
			continue
		}
		if e.IsDir() {
			n += "/"
		}
		out = append(out, n)
		if len(out) == maxTopFiles {
			break
		}
	}
	return out
}

// regularFile reports whether path is a regular file. Lstat, not Stat: a symlink, a device or a
// FIFO is not a file detection may read. A repository can commit `package.json -> /dev/zero` or a
// link to a file outside it, and detection runs inside the daemon.
func regularFile(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode().IsRegular()
}

// dirExists reports whether path is a directory, without following a symlink.
func dirExists(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.IsDir()
}

// readSmall returns the contents of the regular file at path, when it is one and at most
// maxReadBytes long. O_NOFOLLOW makes the open itself refuse a symlink that appears after the
// Lstat above.
func readSmall(path string) ([]byte, bool) {
	if !regularFile(path) {
		return nil, false
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxReadBytes+1))
	if err != nil || len(data) > maxReadBytes {
		return nil, false
	}
	return data, true
}

func anyRegular(dir string, names ...string) bool {
	for _, n := range names {
		if regularFile(filepath.Join(dir, n)) {
			return true
		}
	}
	return false
}
