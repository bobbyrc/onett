package attach

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
	"github.com/bobbyrc/onett/internal/store/sqlite"
)

func newManager(t *testing.T, limit int64) (*Manager, store.Store, string) {
	t.Helper()
	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	dir := filepath.Join(t.TempDir(), "attachments")
	return New(st, dir, func() int64 { return limit }), st, dir
}

func TestSanitizeName(t *testing.T) {
	for in, want := range map[string]string{
		"photo.png":                       "photo.png",
		"../../etc/passwd":                "passwd",
		`..\x`:                            "x",
		`C:\Users\me\a.txt`:               "a.txt",
		"/abs":                            "abs",
		"...":                             "file",
		"..":                              "file",
		".bashrc":                         "bashrc",
		"a\x00b\nc\x7f.txt":               "abc.txt",
		"dir/":                            "file",
		"a..b":                            "ab",
		strings.Repeat("é", 200) + ".png": "",
	} {
		got := SanitizeName(in)
		if want == "" {
			assert.LessOrEqual(t, len(got), maxNameLen)
			assert.True(t, strings.HasSuffix(got, ".png"))
			assert.True(t, strings.ToValidUTF8(got, "") == got, "cut inside a rune")
			continue
		}
		assert.Equal(t, want, got, in)
	}
}

func TestSaveRecordsAndOpens(t *testing.T) {
	m, st, dir := newManager(t, 1<<20)
	ctx := context.Background()
	a, err := m.Save(ctx, "conv1", "../x.txt", strings.NewReader("hello"))
	require.NoError(t, err)
	assert.Equal(t, "x.txt", a.Name)
	assert.Equal(t, "text/plain", a.MIME)
	assert.EqualValues(t, 5, a.Size)
	assert.Equal(t, filepath.Join(dir, "conv1", a.ID+"-x.txt"), a.Path)

	got, err := st.GetAttachment(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, a.Path, got.Path)

	_, f, err := m.Open(ctx, a.ID)
	require.NoError(t, err)
	defer f.Close()
	b := make([]byte, 10)
	n, _ := f.Read(b)
	assert.Equal(t, "hello", string(b[:n]))
}

func TestSaveLimit(t *testing.T) {
	m, st, dir := newManager(t, 600)
	ctx := context.Background()
	_, err := m.Save(ctx, "c", "a.bin", bytes.NewReader(bytes.Repeat([]byte("a"), 601)))
	assert.ErrorIs(t, err, ErrTooLarge)
	_, err = m.Save(ctx, "c", "tiny.bin", bytes.NewReader(bytes.Repeat([]byte("a"), 601)[:10]))
	require.NoError(t, err)
	_, err = m.Save(ctx, "c", "exact.bin", bytes.NewReader(bytes.Repeat([]byte("a"), 600)))
	require.NoError(t, err)
	old, err := st.ListAttachmentsBefore(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Len(t, old, 2, "the refused upload has no record")
	entries, _ := os.ReadDir(filepath.Join(dir, "c"))
	assert.Len(t, entries, 2, "the refused upload left no file")

	// A refusal in an otherwise empty conversation leaves no directory behind either.
	_, err = m.Save(ctx, "fresh", "a.bin", bytes.NewReader(bytes.Repeat([]byte("a"), 700)))
	assert.ErrorIs(t, err, ErrTooLarge)
	_, err = os.Stat(filepath.Join(dir, "fresh"))
	assert.True(t, os.IsNotExist(err))
}

func TestSaveRefusesBadConversationID(t *testing.T) {
	m, _, _ := newManager(t, 100)
	for _, id := range []string{"", "..", ".", "a/b", `a\b`, "../x"} {
		_, err := m.Save(context.Background(), id, "a", strings.NewReader("x"))
		assert.True(t, errors.Is(err, ErrInvalid), id)
	}
}

func TestSniff(t *testing.T) {
	assert.Equal(t, "image/png", Sniff([]byte("\x89PNG\r\n\x1a\n....")))
	assert.Equal(t, "image/jpeg", Sniff([]byte("\xff\xd8\xff\xe0\x00\x10JFIF")))
	assert.Equal(t, "application/pdf", Sniff([]byte("%PDF-1.7")))
	assert.Equal(t, "text/plain", Sniff([]byte("plain text")))
	assert.Equal(t, "image/heic", Sniff([]byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00")))
	assert.Equal(t, "application/octet-stream", Sniff([]byte{0, 1, 2, 3, 0xff}))
}

func TestPruneDeletesOldKeepsNew(t *testing.T) {
	m, st, dir := newManager(t, 1<<20)
	ctx := context.Background()
	now := time.Now()
	save := func(conv, name string, age time.Duration) domain.Attachment {
		m.now = func() time.Time { return now.Add(-age) }
		a, err := m.Save(ctx, conv, name, strings.NewReader("data"))
		require.NoError(t, err)
		return a
	}
	oldA := save("c1", "old.txt", 40*24*time.Hour)
	oldB := save("c2", "old2.txt", 31*24*time.Hour)
	newA := save("c1", "new.txt", 24*time.Hour)

	// A message that carries an old and a new attachment keeps rendering with the new one only.
	msg, err := st.AppendChatMessage(ctx, domain.ChatMessage{
		ConversationID: "c1", Role: "user", Text: "x", Attachments: []domain.Attachment{{ID: oldA.ID}, {ID: newA.ID}},
	})
	require.NoError(t, err)
	require.Len(t, msg.Attachments, 2)

	n, err := m.Prune(ctx, now.Add(-30*24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	for _, a := range []domain.Attachment{oldA, oldB} {
		_, err := os.Stat(a.Path)
		assert.True(t, os.IsNotExist(err), a.Path)
		_, err = st.GetAttachment(ctx, a.ID)
		assert.ErrorIs(t, err, store.ErrNotFound)
	}
	_, err = os.Stat(newA.Path)
	assert.NoError(t, err)
	_, err = st.GetAttachment(ctx, newA.ID)
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "c2"))
	assert.True(t, os.IsNotExist(err), "empty conversation dir is removed")
	_, err = os.Stat(filepath.Join(dir, "c1"))
	assert.NoError(t, err, "a directory that still has a file stays")

	msgs, err := st.ListChatMessages(ctx, "c1")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Len(t, msgs[0].Attachments, 1)
	assert.Equal(t, newA.ID, msgs[0].Attachments[0].ID)

	// Nothing left to prune; a file already missing from disk does not stop the sweep.
	require.NoError(t, os.Remove(newA.Path))
	n, err = m.Prune(ctx, now.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestValidate(t *testing.T) {
	m, _, _ := newManager(t, 1<<20)
	ctx := context.Background()
	a, err := m.Save(ctx, "c1", "a.txt", strings.NewReader("x"))
	require.NoError(t, err)
	b, err := m.Save(ctx, "c2", "b.txt", strings.NewReader("x"))
	require.NoError(t, err)

	got, err := m.Validate(ctx, "c1", []string{a.ID}, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	for name, ids := range map[string][]string{
		"too many": {a.ID, a.ID}, "foreign": {b.ID}, "unknown": {"nope"},
	} {
		_, err := m.Validate(ctx, "c1", ids, 1)
		assert.ErrorIs(t, err, ErrInvalid, name)
	}
}
