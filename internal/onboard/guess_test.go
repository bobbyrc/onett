package onboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTree creates the files of a test repository. A key ending in "/" is a directory; any other
// key is a file with the given content.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, name)
		if strings.HasSuffix(name, "/") {
			require.NoError(t, os.MkdirAll(path, 0o755))
			continue
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
}

const scriptsJSON = `{"scripts":{"lint":"eslint .","typecheck":"tsc","test":"vitest run","build":"vite build"}}`

func TestGuessCommands(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  Guess
	}{
		{
			name:  "npm with a lockfile and scripts",
			files: map[string]string{"package.json": scriptsJSON, "package-lock.json": "{}"},
			want: Guess{
				Setup: []Command{{Name: "npm ci", Run: "npm ci", From: "package-lock.json"}},
				Checks: []Command{
					{Name: "npm run lint", Run: "npm run lint", From: "package.json"},
					{Name: "npm run typecheck", Run: "npm run typecheck", From: "package.json"},
					{Name: "npm test", Run: "npm test", From: "package.json"},
				},
				TopFiles: []string{"package-lock.json", "package.json"},
			},
		},
		{
			name:  "pnpm",
			files: map[string]string{"package.json": `{"scripts":{"test":"vitest"}}`, "pnpm-lock.yaml": ""},
			want: Guess{
				Setup:    []Command{{Name: "pnpm install --frozen-lockfile", Run: "pnpm install --frozen-lockfile", From: "pnpm-lock.yaml"}},
				Checks:   []Command{{Name: "pnpm test", Run: "pnpm test", From: "package.json"}},
				TopFiles: []string{"package.json", "pnpm-lock.yaml"},
			},
		},
		{
			name:  "yarn",
			files: map[string]string{"package.json": `{"scripts":{"test":"jest"}}`, "yarn.lock": ""},
			want: Guess{
				Setup:    []Command{{Name: "yarn install --immutable", Run: "yarn install --immutable", From: "yarn.lock"}},
				Checks:   []Command{{Name: "yarn test", Run: "yarn test", From: "package.json"}},
				TopFiles: []string{"package.json", "yarn.lock"},
			},
		},
		{
			name:  "npm install when there is no lockfile, and no scripts",
			files: map[string]string{"package.json": `{}`},
			want: Guess{
				Setup:    []Command{{Name: "npm install", Run: "npm install", From: "package.json"}},
				Checks:   []Command{},
				TopFiles: []string{"package.json"},
			},
		},
		{
			name:  "bad package.json json means no scripts but still an install",
			files: map[string]string{"package.json": `{not json`, "package-lock.json": "{}"},
			want: Guess{
				Setup:    []Command{{Name: "npm ci", Run: "npm ci", From: "package-lock.json"}},
				Checks:   []Command{},
				TopFiles: []string{"package-lock.json", "package.json"},
			},
		},
		{
			name:  "go module",
			files: map[string]string{"go.mod": "module example.com/x\n"},
			want: Guess{
				Setup: []Command{{Name: "go mod download", Run: "go mod download", From: "go.mod"}},
				Checks: []Command{
					{Name: "go vet ./...", Run: "go vet ./...", From: "go.mod"},
					{Name: "go test ./...", Run: "go test ./...", From: "go.mod"},
				},
				TopFiles: []string{"go.mod"},
			},
		},
		{
			name: "go module with a node web app in a subdirectory",
			files: map[string]string{
				"go.mod":                "module example.com/x\n",
				"web/package.json":      scriptsJSON,
				"web/package-lock.json": "{}",
			},
			want: Guess{
				Setup: []Command{
					{Name: "go mod download", Run: "go mod download", From: "go.mod"},
					{Name: "web: npm ci", Run: "cd web && npm ci", From: "web/package-lock.json"},
				},
				Checks: []Command{
					{Name: "go vet ./...", Run: "go vet ./...", From: "go.mod"},
					{Name: "go test ./...", Run: "go test ./...", From: "go.mod"},
					{Name: "web: npm run lint", Run: "cd web && npm run lint", From: "web/package.json"},
					{Name: "web: npm run typecheck", Run: "cd web && npm run typecheck", From: "web/package.json"},
					{Name: "web: npm test", Run: "cd web && npm test", From: "web/package.json"},
				},
				TopFiles: []string{"go.mod", "web/"},
			},
		},
		{
			name:  "mise.toml prefixes every command",
			files: map[string]string{"mise.toml": "", "go.mod": "module example.com/x\n"},
			want: Guess{
				Setup: []Command{{Name: "go mod download", Run: "mise exec -- go mod download", From: "go.mod"}},
				Checks: []Command{
					{Name: "go vet ./...", Run: "mise exec -- go vet ./...", From: "go.mod"},
					{Name: "go test ./...", Run: "mise exec -- go test ./...", From: "go.mod"},
				},
				TopFiles: []string{"go.mod", "mise.toml"},
			},
		},
		{
			name: "uv with a tests directory",
			files: map[string]string{
				"pyproject.toml": "", "uv.lock": "", "tests/": "",
			},
			want: Guess{
				Setup:    []Command{{Name: "uv sync", Run: "uv sync", From: "uv.lock"}},
				Checks:   []Command{{Name: "uv run pytest", Run: "uv run pytest", From: "tests/"}},
				TopFiles: []string{"pyproject.toml", "tests/", "uv.lock"},
			},
		},
		{
			name:  "cargo",
			files: map[string]string{"Cargo.toml": "[package]\n"},
			want: Guess{
				Setup:    []Command{{Name: "cargo fetch", Run: "cargo fetch", From: "Cargo.toml"}},
				Checks:   []Command{{Name: "cargo test", Run: "cargo test", From: "Cargo.toml"}},
				TopFiles: []string{"Cargo.toml"},
			},
		},
		{
			name:  "makefile test target",
			files: map[string]string{"Makefile": "test:\n\tgo test ./...\n"},
			want: Guess{
				Setup:    []Command{},
				Checks:   []Command{{Name: "make test", Run: "make test", From: "Makefile"}},
				TopFiles: []string{"Makefile"},
			},
		},
		{
			name: "makefile is skipped when the language already gave a check",
			files: map[string]string{
				"go.mod":   "module example.com/x\n",
				"Makefile": "test:\n\tgo test ./...\n",
			},
			want: Guess{
				Setup: []Command{{Name: "go mod download", Run: "go mod download", From: "go.mod"}},
				Checks: []Command{
					{Name: "go vet ./...", Run: "go vet ./...", From: "go.mod"},
					{Name: "go test ./...", Run: "go test ./...", From: "go.mod"},
				},
				TopFiles: []string{"Makefile", "go.mod"},
			},
		},
		{
			name:  "node_modules and dot directories are not scanned",
			files: map[string]string{"node_modules/pkg/go.mod": "module x\n", ".cache/go.mod": "module y\n"},
			want: Guess{
				Setup:    []Command{},
				Checks:   []Command{},
				TopFiles: []string{"node_modules/"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, c.files)
			assert.Equal(t, c.want, GuessCommands(dir))
		})
	}
}

func TestGuessCommandsNothingFoundIsEmptyNotNil(t *testing.T) {
	g := GuessCommands(t.TempDir())
	require.NotNil(t, g.Setup)
	require.NotNil(t, g.Checks)
	require.NotNil(t, g.TopFiles)
	assert.Empty(t, g.Setup)
	assert.Empty(t, g.Checks)
	assert.Empty(t, g.TopFiles)
}

func TestGuessCommandsScansFiveSubdirectoriesInNameOrder(t *testing.T) {
	files := map[string]string{}
	for _, n := range []string{"a1", "a2", "a3", "a4", "a5", "a6"} {
		files[n+"/go.mod"] = "module x\n"
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	var froms []string
	for _, c := range GuessCommands(dir).Setup {
		froms = append(froms, c.From)
	}
	assert.Equal(t, []string{"a1/go.mod", "a2/go.mod", "a3/go.mod", "a4/go.mod", "a5/go.mod"}, froms)
}

func TestGuessTopFilesSortedSkipsDotsMarksDirs(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"zeta.txt": "", "Beta.md": "", "alpha/": "", ".hidden": "", ".github/": "", "dist/": "",
	})
	assert.Equal(t, []string{"Beta.md", "alpha/", "dist/", "zeta.txt"}, GuessCommands(dir).TopFiles)
}

func TestGuessTopFilesAtMostTwelve(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{}
	for i := 0; i < 14; i++ {
		files[string(rune('a'+i))+".txt"] = ""
	}
	writeTree(t, dir, files)
	assert.Len(t, GuessCommands(dir).TopFiles, 12)
}
