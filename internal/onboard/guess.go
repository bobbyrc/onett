package onboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	if anyExists(dir, "mise.toml", ".mise.toml", ".tool-versions") {
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

	if exists(in("package.json")) {
		install, from, pm := nodeInstall(sc.dir)
		setup = append(setup, cmd(install, from))
		scripts := packageScripts(in("package.json"))
		for _, script := range []string{"lint", "typecheck", "test"} {
			if _, ok := scripts[script]; ok {
				checks = append(checks, cmd(nodeRun(pm, script), "package.json"))
			}
		}
	}
	if exists(in("go.mod")) {
		setup = append(setup, cmd("go mod download", "go.mod"))
		checks = append(checks, cmd("go vet ./...", "go.mod"), cmd("go test ./...", "go.mod"))
	}
	switch {
	case exists(in("pyproject.toml")) && exists(in("uv.lock")):
		setup = append(setup, cmd("uv sync", "uv.lock"))
		if marker := pytestMarker(sc.dir); marker != "" {
			checks = append(checks, cmd("uv run pytest", marker))
		}
	case exists(in("poetry.lock")):
		setup = append(setup, cmd("poetry install", "poetry.lock"))
		if marker := pytestMarker(sc.dir); marker != "" {
			checks = append(checks, cmd("poetry run pytest", marker))
		}
	case exists(in("requirements.txt")):
		setup = append(setup, cmd("pip install -r requirements.txt", "requirements.txt"))
		if marker := pytestMarker(sc.dir); marker != "" {
			checks = append(checks, cmd("python -m pytest", marker))
		}
	}
	if exists(in("Cargo.toml")) {
		setup = append(setup, cmd("cargo fetch", "Cargo.toml"))
		checks = append(checks, cmd("cargo test", "Cargo.toml"))
	}
	// A Makefile's test target is the fallback: it is used only when the language files gave no check.
	if len(checks) == 0 {
		for _, mk := range []string{"Makefile", "makefile", "GNUmakefile"} {
			if data, err := os.ReadFile(in(mk)); err == nil && makeTest.Match(data) {
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
		if exists(filepath.Join(dir, l.file)) {
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
	data, err := os.ReadFile(path)
	if err != nil {
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
	for _, m := range []string{"tests/", "pytest.ini", "conftest.py"} {
		if exists(filepath.Join(dir, strings.TrimSuffix(m, "/"))) {
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
		if !e.IsDir() || strings.HasPrefix(n, ".") || skipDirs[n] {
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

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func anyExists(dir string, names ...string) bool {
	for _, n := range names {
		if exists(filepath.Join(dir, n)) {
			return true
		}
	}
	return false
}
