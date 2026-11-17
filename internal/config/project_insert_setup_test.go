package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddProjectWritesSetupAndChecks(t *testing.T) {
	base := "# top\nprojects:\n  app:\n    repo: acme/app\n"
	setup := []Check{{Name: "npm ci", Run: "npm ci"}}
	checks := []Check{{Name: "npm test", Run: "npm test"}, {Name: "lint", Run: "npm run lint"}}
	out, err := AddProject([]byte(base), "onett.yaml", NewProject{
		Name: "web", Repo: "acme/web", Policy: "default", Setup: setup, Checks: checks,
	})
	require.NoError(t, err)
	assert.Contains(t, string(out), "# top")
	assert.Contains(t, string(out), "    setup:\n      - name: npm ci\n        run: npm ci\n")
	assert.Contains(t, string(out), "    checks:\n      - name: npm test\n        run: npm test\n")

	cfg, err := Parse(out, "onett.yaml")
	require.NoError(t, err)
	assert.Equal(t, setup, cfg.Projects["web"].Setup)
	assert.Equal(t, checks, cfg.Projects["web"].Checks)
}

func TestAddProjectQuotesCommandValues(t *testing.T) {
	setup := []Check{{Name: "build: web", Run: "cd web && npm ci # install"}}
	checks := []Check{{Name: "# hash", Run: "echo 'a: b' && test -n \"$X\""}}
	out, err := AddProject([]byte("projects:\n  app:\n    repo: acme/app\n"), "onett.yaml", NewProject{
		Name: "web", Repo: "acme/web", Setup: setup, Checks: checks,
	})
	require.NoError(t, err)
	cfg, err := Parse(out, "onett.yaml")
	require.NoError(t, err)
	assert.Equal(t, setup, cfg.Projects["web"].Setup)
	assert.Equal(t, checks, cfg.Projects["web"].Checks)
}

func TestAddProjectRefusesBadSetupOrChecks(t *testing.T) {
	base := []byte("projects:\n  app:\n    repo: acme/app\n")
	for _, tc := range []struct {
		name string
		p    NewProject
		want string
	}{
		{"empty setup name", NewProject{Setup: []Check{{Name: " ", Run: "npm ci"}}}, "setup command 1 needs a name and a command"},
		{"empty setup run", NewProject{Setup: []Check{{Name: "a", Run: "x"}, {Name: "b", Run: ""}}}, "setup command 2 needs a name and a command"},
		{"empty checks run", NewProject{Checks: []Check{{Name: "t", Run: "\t"}}}, "checks command 1 needs a name and a command"},
		{"duplicate setup", NewProject{Setup: []Check{{Name: "a", Run: "x"}, {Name: "a", Run: "y"}}}, `setup has the name "a" twice`},
		{"duplicate checks", NewProject{Checks: []Check{{Name: "t", Run: "x"}, {Name: "t", Run: "y"}}}, `checks has the name "t" twice`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.p
			p.Name, p.Repo = "web", "acme/web"
			out, err := AddProject(base, "onett.yaml", p)
			require.EqualError(t, err, tc.want)
			assert.Nil(t, out)
		})
	}
}

func TestAddProjectPolicyDefaultsToSupervised(t *testing.T) {
	base := []byte("projects:\n  app:\n    repo: acme/app\n")
	out, err := AddProject(base, "onett.yaml", NewProject{Name: "web", Repo: "acme/web"})
	require.NoError(t, err)
	cfg, err := Parse(out, "onett.yaml")
	require.NoError(t, err)
	assert.Equal(t, "supervised", cfg.Projects["web"].Policy)

	out, err = AddProject(base, "onett.yaml", NewProject{Name: "web", Repo: "acme/web", Policy: "default"})
	require.NoError(t, err)
	cfg, err = Parse(out, "onett.yaml")
	require.NoError(t, err)
	assert.Equal(t, "default", cfg.Projects["web"].Policy)
}
