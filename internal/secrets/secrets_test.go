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
	assert.Equal(t, []Pair{
		{"FOO", "bar"},
		{"BAZ", "quoted value"},
		{"QUX", "single"},
		{"EMPTY", ""},
		{"SPACED", "padded"},
	}, Parse(data))
}

func TestSetKeepsOtherLinesAndFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "secrets.env")

	require.NoError(t, Set(path, "A_TOKEN", "one"))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, os.WriteFile(path, []byte("# keep me\nexport A_TOKEN=old\nB=2\nA_TOKEN=dup\n"), 0o600))
	require.NoError(t, Set(path, "A_TOKEN", "new"))
	got, _ := os.ReadFile(path)
	assert.Equal(t, "# keep me\nA_TOKEN=new\nB=2\n", string(got), "replaced in place, repeat dropped")

	require.NoError(t, Set(path, "C", "3"))
	names, err := Names(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"A_TOKEN", "B", "C"}, names)
}

func TestSetQuotesOnlyWhenNeeded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	for _, v := range []string{"plain", " padded ", `"already"`, "it's", "a#b=c"} {
		require.NoError(t, Set(path, "K", v))
		data, _ := os.ReadFile(path)
		pairs := Parse(data)
		require.Len(t, pairs, 1)
		assert.Equal(t, v, pairs[0].Value, "round trip of %q", v)
	}
	data, _ := os.ReadFile(path)
	assert.Equal(t, "K=a#b=c\n", string(data))
}

func TestSetRefusesBadInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	for _, tc := range []struct{ name, value string }{
		{"1BAD", "x"}, {"has space", "x"}, {"", "x"}, {"OK", ""}, {"OK", "a\nb"}, {"OK", `'a"b'`},
	} {
		err := Set(path, tc.name, tc.value)
		assert.ErrorIs(t, err, ErrInvalid, "%q=%q", tc.name, tc.value)
	}
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "a refused write leaves no file")
}

func TestDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	require.NoError(t, os.WriteFile(path, []byte("A=1\nB=2\n"), 0o600))
	found, err := Delete(path, "A")
	require.NoError(t, err)
	assert.True(t, found)
	found, err = Delete(path, "A")
	require.NoError(t, err)
	assert.False(t, found)
	names, _ := Names(path)
	assert.Equal(t, []string{"B"}, names)
}

func TestNamesMissingFile(t *testing.T) {
	names, err := Names(filepath.Join(t.TempDir(), "none"))
	require.NoError(t, err)
	assert.Empty(t, names)
	assert.NotNil(t, names)
}
