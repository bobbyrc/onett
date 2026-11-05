package telegram

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// media builds a private-chat update from a user carrying the given extra message JSON.
func (f *fakeAPI) media(userID int64, extra string) string {
	f.mu.Lock()
	f.nextUp++
	id := f.nextUp
	f.mu.Unlock()
	return fmt.Sprintf(`{"update_id":%d,"message":{"message_id":1,"from":{"id":%d},"chat":{"id":%d,"type":"private"},%s}}`,
		id, userID, userID, extra)
}

func (f *fakeAPI) downloads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fileGet
}

func TestPhotoWithCaptionIsSavedAndSent(t *testing.T) {
	api, host := newFakeAPI(t), newFakeHost()
	api.files = map[string][]byte{"big": []byte("BIGPIXELS"), "small": []byte("tiny")}
	api.push(api.media(42, `"caption":"what is this?","photo":[{"file_id":"small","file_size":4},{"file_id":"big","file_size":9}]`))
	h := start(t, api, host, filepath.Join(t.TempDir(), "s.json"))
	waitFor(t, "chat with attachment", func() bool { host.mu.Lock(); defer host.mu.Unlock(); return len(host.sentWith) == 1 })
	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.saved) != 1 || string(host.saved[0].data) != "BIGPIXELS" || host.saved[0].name != "big" {
		t.Fatalf("saved = %+v, want the largest size", host.saved)
	}
	sw := host.sentWith[0]
	if sw.text != "what is this?" || len(sw.ids) != 1 || sw.ids[0] != "att1" || sw.conv != host.saved[0].conv {
		t.Fatalf("sentWith = %+v", sw)
	}
	if strings.Contains(h.logs.String(), testToken) {
		t.Fatalf("token in logs:\n%s", h.logs.String())
	}
}

func TestDocumentIsSavedUnderItsName(t *testing.T) {
	api, host := newFakeAPI(t), newFakeHost()
	api.files = map[string][]byte{"d1": []byte("%PDF-1.4")}
	api.push(api.media(42, `"document":{"file_id":"d1","file_size":8,"file_name":"report.pdf"}`))
	start(t, api, host, filepath.Join(t.TempDir(), "s.json"))
	waitFor(t, "chat with attachment", func() bool { host.mu.Lock(); defer host.mu.Unlock(); return len(host.sentWith) == 1 })
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.saved[0].name != "report.pdf" || host.sentWith[0].text != "" {
		t.Fatalf("saved=%+v sentWith=%+v", host.saved, host.sentWith)
	}
}

func TestOversizeDeclaredIsRejectedWithoutDownload(t *testing.T) {
	api, host := newFakeAPI(t), newFakeHost()
	host.maxSize = 25 << 20
	api.push(api.media(42, `"document":{"file_id":"d1","file_size":41943040,"file_name":"huge.bin"}`))
	start(t, api, host, filepath.Join(t.TempDir(), "s.json"))
	waitFor(t, "rejection", func() bool { return len(api.texts()) == 1 })
	if got := api.texts()[0]; got != "That file is 40 MB; the limit is 25 MB." {
		t.Fatalf("reply = %q", got)
	}
	if n := len(api.byMethod("getFile")); n != 0 || api.downloads() != 0 {
		t.Fatalf("getFile calls=%d downloads=%d, want none", n, api.downloads())
	}
	if len(host.saved) != 0 || len(host.sentWith) != 0 {
		t.Fatal("something was saved or sent")
	}
}

func TestOverTelegramBotLimitExplainsIt(t *testing.T) {
	api, host := newFakeAPI(t), newFakeHost()
	host.maxSize = 100 << 20
	api.push(api.media(42, `"video":{"file_id":"v","file_size":31457280}`))
	start(t, api, host, filepath.Join(t.TempDir(), "s.json"))
	waitFor(t, "explanation", func() bool { return len(api.texts()) == 1 })
	if got := api.texts()[0]; !strings.Contains(got, "30 MB") || !strings.Contains(got, "20 MB") {
		t.Fatalf("reply = %q", got)
	}
	if len(api.byMethod("getFile")) != 0 {
		t.Fatal("downloaded a file Telegram will not serve")
	}
}

func TestOversizeByActualStreamIsRejectedAndNothingSaved(t *testing.T) {
	api, host := newFakeAPI(t), newFakeHost()
	host.maxSize = 1024
	api.files = map[string][]byte{"liar": bytes.Repeat([]byte("a"), 5000)}
	// The declared size is small; the real body is not.
	api.push(api.media(42, `"document":{"file_id":"liar","file_size":10,"file_name":"x.bin"}`))
	start(t, api, host, filepath.Join(t.TempDir(), "s.json"))
	waitFor(t, "rejection", func() bool { return len(api.texts()) == 1 })
	if got := api.texts()[0]; !strings.Contains(got, "limit") {
		t.Fatalf("reply = %q", got)
	}
	if len(host.saved) != 0 || len(host.sentWith) != 0 {
		t.Fatal("an oversize stream was kept")
	}
}

func TestNotAllowedUsersFileIsNeverDownloaded(t *testing.T) {
	api, host := newFakeAPI(t), newFakeHost()
	api.files = map[string][]byte{"d1": []byte("data")}
	api.push(
		api.media(7, `"document":{"file_id":"d1","file_size":4,"file_name":"a.txt"}`),
		api.msg(42, 42, "private", "sync"),
	)
	start(t, api, host, filepath.Join(t.TempDir(), "s.json"))
	waitFor(t, "allowed message", func() bool { s, _, _ := host.snapshot(); return len(s) == 1 })
	if len(api.byMethod("getFile")) != 0 || api.downloads() != 0 || len(host.saved) != 0 {
		t.Fatal("a stranger's file was fetched")
	}
}

func TestMessageWithoutTextOrFileGetsHelp(t *testing.T) {
	api, host := newFakeAPI(t), newFakeHost()
	api.push(api.media(42, `"sticker":{"file_id":"s"}`))
	start(t, api, host, filepath.Join(t.TempDir(), "s.json"))
	waitFor(t, "help", func() bool { return len(api.texts()) == 1 })
	if got := api.texts()[0]; strings.Contains(got, "Only text") || !strings.Contains(got, "photos and files") {
		t.Fatalf("reply = %q", got)
	}
}

func TestDownloadFailureNeverLogsToken(t *testing.T) {
	api, host := newFakeAPI(t), newFakeHost()
	// getFile succeeds but the file is missing on the file server: HTTP 404.
	api.push(api.media(42, `"document":{"file_id":"gone","file_size":4,"file_name":"a.txt"}`))
	h := start(t, api, host, filepath.Join(t.TempDir(), "s.json"))
	waitFor(t, "failure reply", func() bool { return len(api.texts()) == 1 })
	if strings.Contains(h.logs.String(), testToken) || strings.Contains(api.texts()[0], testToken) {
		t.Fatalf("token leaked: logs=%s reply=%s", h.logs.String(), api.texts()[0])
	}
	if !strings.Contains(h.logs.String(), "download failed") {
		t.Fatalf("failure not logged:\n%s", h.logs.String())
	}
}
