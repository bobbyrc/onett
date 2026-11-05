package secrets

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	data := []byte(`# comment

FOO=bar
export BAZ="quoted value"
QUX='single'
EMPTY=
=novalue
NOEQUALS
  SPACED = padded  
`)
	assert.Equal(t, []entry{
		{"FOO", "bar"}, {"BAZ", "quoted value"}, {"QUX", "single"}, {"EMPTY", ""}, {"SPACED", "padded"},
	}, parse(data))
}

func TestLoadKeepsRealEnvironmentAndMissingFileIsFine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	require.NoError(t, os.WriteFile(path, []byte("ONETT_TEST_FROM_FILE=file-value\nONETT_TEST_ALREADY_SET=file-value\n"), 0o600))
	t.Setenv("ONETT_TEST_ALREADY_SET", "env-value")
	t.Cleanup(func() { _ = os.Unsetenv("ONETT_TEST_FROM_FILE") })

	s := New(path)
	require.NoError(t, s.Load())
	assert.Equal(t, "file-value", os.Getenv("ONETT_TEST_FROM_FILE"))
	assert.Equal(t, "env-value", os.Getenv("ONETT_TEST_ALREADY_SET"))
	assert.Equal(t, "file", s.Source("ONETT_TEST_FROM_FILE"))
	assert.Equal(t, "environment", s.Source("ONETT_TEST_ALREADY_SET"))
	assert.Equal(t, "", s.Source("ONETT_TEST_NOPE"))

	assert.NoError(t, New(filepath.Join(t.TempDir(), "nope.env")).Load())
}

func TestReloadFollowsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	require.NoError(t, os.WriteFile(path, []byte("ONETT_TEST_A=1\nONETT_TEST_B=2\n"), 0o600))
	t.Cleanup(func() { _ = os.Unsetenv("ONETT_TEST_A"); _ = os.Unsetenv("ONETT_TEST_B") })
	s := New(path)
	require.NoError(t, s.Load())

	require.NoError(t, os.WriteFile(path, []byte("ONETT_TEST_A=changed\n"), 0o600))
	require.NoError(t, s.Load())
	assert.Equal(t, "changed", os.Getenv("ONETT_TEST_A"))
	_, bSet := os.LookupEnv("ONETT_TEST_B")
	assert.False(t, bSet, "a name the file dropped is unset")
}

func TestSetCreatesFileAtomicallyWithModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	path := filepath.Join(dir, "secrets.env")
	t.Cleanup(func() { _ = os.Unsetenv("ONETT_TEST_NEW") })
	s := New(path)

	shadowed, err := s.Set("ONETT_TEST_NEW", "v1")
	require.NoError(t, err)
	assert.False(t, shadowed)
	assert.Equal(t, "v1", os.Getenv("ONETT_TEST_NEW"))

	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	di, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), di.Mode().Perm())
	entries, _ := os.ReadDir(dir)
	assert.Len(t, entries, 1, "no temp file is left behind")
}

func TestSetKeepsOtherLinesAndReplacesInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	require.NoError(t, os.WriteFile(path, []byte("# my tokens\nONETT_TEST_A=old\n\nexport ONETT_TEST_B=keep\nONETT_TEST_A=dup\n"), 0o644))
	t.Cleanup(func() { _ = os.Unsetenv("ONETT_TEST_A") })
	s := New(path)

	_, err := s.Set("ONETT_TEST_A", "new")
	require.NoError(t, err)
	got, _ := os.ReadFile(path)
	assert.Equal(t, "# my tokens\nONETT_TEST_A=new\n\nexport ONETT_TEST_B=keep\n", string(got))
	fi, _ := os.Stat(path)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "a loose file is tightened")
}

func TestSetShadowedByRealEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	t.Setenv("ONETT_TEST_REAL", "from-env")
	s := New(path)

	shadowed, err := s.Set("ONETT_TEST_REAL", "from-file")
	require.NoError(t, err)
	assert.True(t, shadowed)
	assert.Equal(t, "from-env", os.Getenv("ONETT_TEST_REAL"))
	got, _ := os.ReadFile(path)
	assert.Equal(t, "ONETT_TEST_REAL=from-file\n", string(got), "the file still records it")
	assert.Equal(t, "environment", s.Source("ONETT_TEST_REAL"))

	require.NoError(t, s.Delete("ONETT_TEST_REAL"))
	assert.Equal(t, "from-env", os.Getenv("ONETT_TEST_REAL"), "deleting never unsets the real environment")
}

func TestSetRejectsBadInput(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "secrets.env"))
	for _, name := range []string{"", "lower", "1ABC", "A-B", "A B"} {
		_, err := s.Set(name, "x")
		assert.ErrorIs(t, err, ErrInvalid, name)
	}
	for _, v := range []string{"", "a\nb", "a\rb"} {
		_, err := s.Set("ONETT_TEST_X", v)
		assert.ErrorIs(t, err, ErrInvalid)
	}
}

func TestValuesRoundTripThroughTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	t.Cleanup(func() { _ = os.Unsetenv("ONETT_TEST_RT") })
	s := New(path)
	for _, v := range []string{"plain", " padded ", `"quoted"`, `'single'`, "a=b#c", `has "inner" quotes`} {
		_, err := s.Set("ONETT_TEST_RT", v)
		require.NoError(t, err)
		_ = os.Unsetenv("ONETT_TEST_RT")
		s2 := New(path)
		require.NoError(t, s2.Load())
		assert.Equal(t, v, os.Getenv("ONETT_TEST_RT"), v)
		s = s2
	}
}

func TestDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	require.NoError(t, os.WriteFile(path, []byte("# keep\nONETT_TEST_D=1\nONETT_TEST_E=2\n"), 0o600))
	t.Cleanup(func() { _ = os.Unsetenv("ONETT_TEST_D"); _ = os.Unsetenv("ONETT_TEST_E") })
	s := New(path)
	require.NoError(t, s.Load())

	require.NoError(t, s.Delete("ONETT_TEST_D"))
	got, _ := os.ReadFile(path)
	assert.Equal(t, "# keep\nONETT_TEST_E=2\n", string(got))
	_, set := os.LookupEnv("ONETT_TEST_D")
	assert.False(t, set)

	assert.ErrorIs(t, s.Delete("ONETT_TEST_D"), ErrNotFound)
	assert.ErrorIs(t, New(filepath.Join(t.TempDir(), "none.env")).Delete("ONETT_TEST_D"), ErrNotFound)
	names, err := s.Names()
	require.NoError(t, err)
	assert.Equal(t, []string{"ONETT_TEST_E"}, names)
}

func TestSetWritesThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.env")
	link := filepath.Join(dir, "secrets.env")
	require.NoError(t, os.WriteFile(real, []byte("ONETT_TEST_L=1\n"), 0o600))
	require.NoError(t, os.Symlink(real, link))
	t.Cleanup(func() { _ = os.Unsetenv("ONETT_TEST_L"); _ = os.Unsetenv("ONETT_TEST_M") })

	_, err := New(link).Set("ONETT_TEST_M", "2")
	require.NoError(t, err)
	fi, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "the link survives")
	got, _ := os.ReadFile(real)
	assert.Equal(t, "ONETT_TEST_L=1\nONETT_TEST_M=2\n", string(got))
}

func TestReferences(t *testing.T) {
	doc := map[string]any{
		"channels": []any{map[string]any{"type": "telegram", "options": map[string]any{"bot_token_env": "TELEGRAM_BOT_TOKEN"}}},
		"notify": []any{map[string]any{"options": map[string]any{
			"webhook_url": "${DISCORD_URL}", "note": "${DISCORD_URL} and ${DISCORD_URL}",
		}}},
		"harness": map[string]any{"x": map[string]any{"inherit_env": []any{"HOME"}}},
		"bad":     map[string]any{"token_env": "not a name"},
	}
	assert.Equal(t, map[string][]string{
		"TELEGRAM_BOT_TOKEN": {"channels[0].options.bot_token_env"},
		"DISCORD_URL":        {"notify[0].options.note", "notify[0].options.webhook_url"},
	}, References(doc))
}
