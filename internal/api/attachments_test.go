package api

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/attach"
	"github.com/bobbyrc/onett/internal/domain"
)

func (f *fakeCtl) SendChatWith(_ context.Context, convID, text string, ids []string) (domain.ChatMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatConvID, f.chatText, f.chatAtts = convID, text, ids
	if err := f.note("SendChat"); err != nil {
		return domain.ChatMessage{}, err
	}
	return domain.ChatMessage{ID: "msg-1", ConversationID: convID, Role: "user", Text: text}, nil
}

var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")

type attachEnv struct {
	x      *harness
	dir    string
	cookie string
	conv   domain.Conversation
}

func newAttachEnv(t *testing.T, maxSize int64) *attachEnv {
	t.Helper()
	x := newHarness(t)
	dir := t.TempDir()
	x.ctl.cfg.Chat.Attachments.MaxPerMessage = 2
	m := attach.New(x.st, dir, func() int64 { return maxSize })
	x.h = NewServer(Options{
		Store: x.st, Bus: x.bus, Controller: x.ctl, Token: testToken, Version: "test",
		Web: fstest.MapFS{}, Attachments: m, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	conv, err := x.st.CreateConversation(context.Background(), domain.Conversation{Title: "c"})
	require.NoError(t, err)
	return &attachEnv{x: x, dir: dir, cookie: x.newUserSession(t, "alice", domain.RoleOperator), conv: conv}
}

// upload sends a multipart upload of content named filename in the field "file".
func (e *attachEnv) upload(t *testing.T, convID, filename string, content []byte, opts ...reqOpt) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	req := testRequest(http.MethodPost, "/api/v1/conversations/"+convID+"/attachments", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	e.x.h.ServeHTTP(rec, req)
	return rec
}

func (e *attachEnv) uploadOK(t *testing.T, filename string, content []byte) attachmentView {
	t.Helper()
	rec := e.upload(t, e.conv.ID, filename, content, withCookie(e.cookie))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var v attachmentView
	decode(t, rec, &v)
	return v
}

func filesUnder(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	require.NoError(t, filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return err
	}))
	return out
}

func TestUploadAttachment(t *testing.T) {
	e := newAttachEnv(t, 1<<20)
	v := e.uploadOK(t, "photo.png", pngBytes)
	assert.NotEmpty(t, v.ID)
	assert.Equal(t, "photo.png", v.Name)
	assert.Equal(t, "image/png", v.MIME)
	assert.Equal(t, int64(len(pngBytes)), v.Size)

	files := filesUnder(t, e.dir)
	require.Len(t, files, 1)
	assert.True(t, strings.HasPrefix(files[0], filepath.Join(e.dir, e.conv.ID)+string(filepath.Separator)))
	assert.Equal(t, v.ID+"-photo.png", filepath.Base(files[0]))
	fi, err := os.Stat(files[0])
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	di, err := os.Stat(filepath.Dir(files[0]))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), di.Mode().Perm())

	// The on-disk path never reaches the client.
	rec := e.upload(t, e.conv.ID, "b.png", pngBytes, withCookie(e.cookie))
	assert.NotContains(t, rec.Body.String(), e.dir)
	assert.NotContains(t, rec.Body.String(), "path")
}

func TestUploadOverLimitIs413AndLeavesNothing(t *testing.T) {
	e := newAttachEnv(t, 100)
	rec := e.upload(t, e.conv.ID, "big.bin", bytes.Repeat([]byte("a"), 5000), withCookie(e.cookie))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
	assert.Empty(t, filesUnder(t, e.dir))
	// A file of exactly the limit is accepted.
	rec = e.upload(t, e.conv.ID, "ok.bin", bytes.Repeat([]byte("a"), 100), withCookie(e.cookie))
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	// Far over the limit: the request itself is capped.
	rec = e.upload(t, e.conv.ID, "huge.bin", bytes.Repeat([]byte("a"), 3<<20), withCookie(e.cookie))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Len(t, filesUnder(t, e.dir), 1)
}

func TestUploadSniffsTypeNotNameOrClient(t *testing.T) {
	e := newAttachEnv(t, 1<<20)
	assert.Equal(t, "image/png", e.uploadOK(t, "notes.txt", pngBytes).MIME)
	assert.Equal(t, "text/plain", e.uploadOK(t, "pic.png", []byte("just some words")).MIME)
	assert.Equal(t, "application/pdf", e.uploadOK(t, "x.bin", []byte("%PDF-1.7\n1 0 obj")).MIME)
}

func TestUploadTraversalNamesStayInside(t *testing.T) {
	e := newAttachEnv(t, 1<<20)
	for name, want := range map[string]string{
		"../../etc/passwd": "passwd",
		`..\x`:             "x",
		"/abs":             "abs",
		"..":               "file",
		".hidden":          "hidden",
		"":                 "file",
	} {
		v := e.uploadOK(t, name, []byte("data"))
		assert.Equal(t, want, v.Name, name)
	}
	for _, f := range filesUnder(t, e.dir) {
		rel, err := filepath.Rel(e.dir, f)
		require.NoError(t, err)
		assert.False(t, strings.HasPrefix(rel, ".."), f)
		assert.Equal(t, 2, len(strings.Split(rel, string(filepath.Separator))), rel)
	}
	_, err := os.Stat(filepath.Join(filepath.Dir(e.dir), "passwd"))
	assert.True(t, os.IsNotExist(err))
}

func TestAttachmentsNeedASignedInPerson(t *testing.T) {
	e := newAttachEnv(t, 1<<20)
	v := e.uploadOK(t, "a.png", pngBytes)
	dl := "/api/v1/conversations/" + e.conv.ID + "/attachments/" + v.ID

	rec := e.upload(t, e.conv.ID, "a.png", pngBytes)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	rec = e.upload(t, e.conv.ID, "a.png", pngBytes, withToken(testToken))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, http.StatusUnauthorized, e.x.authReq(t, http.MethodGet, dl, "").Code)
	assert.Equal(t, http.StatusForbidden, e.x.authReq(t, http.MethodGet, dl, "", withToken(testToken)).Code)
	assert.Equal(t, http.StatusOK, e.x.authReq(t, http.MethodGet, dl, "", withCookie(e.cookie)).Code)
	assert.Len(t, filesUnder(t, e.dir), 1)

	viewer := e.x.newUserSession(t, "watcher", domain.RoleViewer)
	assert.Equal(t, http.StatusForbidden, e.upload(t, e.conv.ID, "a.png", pngBytes, withCookie(viewer)).Code)
}

func TestUploadRefusesBadTargets(t *testing.T) {
	e := newAttachEnv(t, 1<<20)
	assert.Equal(t, http.StatusNotFound, e.upload(t, "nope", "a.png", pngBytes, withCookie(e.cookie)).Code)

	tc, err := e.x.st.CreateConversation(context.Background(), domain.Conversation{TaskID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, e.upload(t, tc.ID, "a.png", pngBytes, withCookie(e.cookie)).Code)

	rec := e.x.authReq(t, http.MethodPost, "/api/v1/conversations/"+e.conv.ID+"/attachments", `{"x":1}`, withCookie(e.cookie))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, http.StatusBadRequest, e.upload(t, e.conv.ID, "empty.txt", nil, withCookie(e.cookie)).Code)
	assert.Empty(t, filesUnder(t, e.dir))
}

func TestDownloadHeaders(t *testing.T) {
	e := newAttachEnv(t, 1<<20)
	get := func(v attachmentView) *httptest.ResponseRecorder {
		return e.x.authReq(t, http.MethodGet, "/api/v1/conversations/"+e.conv.ID+"/attachments/"+v.ID, "", withCookie(e.cookie))
	}

	img := get(e.uploadOK(t, "a.png", pngBytes))
	require.Equal(t, http.StatusOK, img.Code)
	assert.Equal(t, "image/png", img.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", img.Header().Get("X-Content-Type-Options"))
	assert.True(t, strings.HasPrefix(img.Header().Get("Content-Disposition"), "inline"), img.Header().Get("Content-Disposition"))
	assert.Equal(t, pngBytes, img.Body.Bytes())

	pdf := get(e.uploadOK(t, "a.pdf", []byte("%PDF-1.4 body")))
	assert.True(t, strings.HasPrefix(pdf.Header().Get("Content-Disposition"), "inline"))

	for name, content := range map[string]string{
		"notes.txt": "plain words",
		"page.html": "<html><script>alert(1)</script></html>",
		"pic.svg":   `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"blob.bin":  "\x00\x01\x02\x03",
	} {
		rec := get(e.uploadOK(t, name, []byte(content)))
		require.Equal(t, http.StatusOK, rec.Code, name)
		assert.True(t, strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment"), "%s: %s", name, rec.Header().Get("Content-Disposition"))
		assert.Contains(t, rec.Header().Get("Content-Disposition"), name)
		assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
		assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "sandbox")
	}
}

func TestDownloadFromAnotherConversationIs404(t *testing.T) {
	e := newAttachEnv(t, 1<<20)
	v := e.uploadOK(t, "a.png", pngBytes)
	other, err := e.x.st.CreateConversation(context.Background(), domain.Conversation{Title: "other"})
	require.NoError(t, err)
	rec := e.x.authReq(t, http.MethodGet, "/api/v1/conversations/"+other.ID+"/attachments/"+v.ID, "", withCookie(e.cookie))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = e.x.authReq(t, http.MethodGet, "/api/v1/conversations/"+e.conv.ID+"/attachments/nope", "", withCookie(e.cookie))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestMessageWithAttachments(t *testing.T) {
	e := newAttachEnv(t, 1<<20)
	a := e.uploadOK(t, "a.png", pngBytes)
	b := e.uploadOK(t, "b.png", pngBytes)
	c := e.uploadOK(t, "c.png", pngBytes)
	post := func(body string) *httptest.ResponseRecorder {
		return e.x.authReq(t, http.MethodPost, "/api/v1/conversations/"+e.conv.ID+"/messages", body, withCookie(e.cookie))
	}

	rec := post(`{"text":"look","attachments":["` + a.ID + `","` + b.ID + `"]}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Equal(t, []string{a.ID, b.ID}, e.x.ctl.chatAtts)
	// Text may be empty only with attachments.
	assert.Equal(t, http.StatusAccepted, post(`{"attachments":["`+a.ID+`"]}`).Code)
	assert.Equal(t, http.StatusBadRequest, post(`{"text":""}`).Code)

	// More than max_per_message (2 here).
	rec = post(`{"text":"x","attachments":["` + a.ID + `","` + b.ID + `","` + c.ID + `"]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "at most 2")
	// Unknown id, a repeat, and an attachment of another conversation.
	assert.Equal(t, http.StatusBadRequest, post(`{"text":"x","attachments":["nope"]}`).Code)
	assert.Equal(t, http.StatusBadRequest, post(`{"text":"x","attachments":["`+a.ID+`","`+a.ID+`"]}`).Code)
	other, err := e.x.st.CreateConversation(context.Background(), domain.Conversation{Title: "other"})
	require.NoError(t, err)
	rec = e.x.authReq(t, http.MethodPost, "/api/v1/conversations/"+other.ID+"/messages",
		`{"text":"x","attachments":["`+a.ID+`"]}`, withCookie(e.cookie))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
