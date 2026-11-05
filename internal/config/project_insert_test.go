package config

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddProjectWritesOptionalFieldsAndInstructions(t *testing.T) {
	base := "# top\nprojects:\n  app:\n    repo: acme/app\n"
	for _, instr := range []string{
		"one line",
		"two\nlines",
		"blank\n\nbetween",
		"  leading space\nsecond",
		"ends with newline\n",
		"tab\there\nx",
		"colon: and # hash\nsecond: line",
	} {
		out, err := AddProject([]byte(base), "onett.yaml", NewProject{
			Name: "web", Repo: "acme/web", Path: "/srv/web", Branch: "dev", Policy: "default", Instructions: instr,
		})
		require.NoError(t, err, instr)
		cfg, err := Parse(out, "onett.yaml")
		require.NoError(t, err, instr)
		p := cfg.Projects["web"]
		assert.Equal(t, instr, p.Instructions)
		assert.Equal(t, "/srv/web", p.Path)
		assert.Equal(t, "dev", p.DefaultBranch)
		assert.Contains(t, string(out), "# top")
	}
}

func TestAddProjectErrors(t *testing.T) {
	base := []byte("projects:\n  app:\n    repo: acme/app\n")
	_, err := AddProject(base, "c.yaml", NewProject{Name: "app", Repo: "acme/x"})
	assert.True(t, errors.Is(err, ErrProjectExists))

	_, err = AddProject(base, "c.yaml", NewProject{Name: "bad name", Repo: "acme/x"})
	assert.Error(t, err)

	_, err = AddProject(base, "c.yaml", NewProject{Name: "x", Repo: "acme/x", Policy: "nope"})
	var invalid *InvalidResultError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "projects.x.policy", ProblemsFrom(invalid.Err)[0].Path)
}
