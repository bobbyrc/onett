package discord

import (
	"fmt"
	"strings"
	"testing"
)

func (f *fakeDiscord) dmWith(id, channel, userID, content string, files ...map[string]any) map[string]any {
	m := f.dm(id, channel, userID, content)
	m["attachments"] = files
	return m
}

func (f *fakeDiscord) file(name string, declared int, served int) map[string]any {
	f.mu.Lock()
	if f.cdnSizes == nil {
		f.cdnSizes = map[string]int{}
	}
	f.cdnSizes[name] = served
	f.mu.Unlock()
	return map[string]any{"filename": name, "size": declared, "url": f.srv.URL + "/cdn/" + name + "?ex=SIGNEDSECRET"}
}

func (f *fakeDiscord) downloaded() []download {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]download(nil), f.downloads...)
}

func TestAttachmentsAreSavedAndSentTogether(t *testing.T) {
	f, host := newFakeDiscord(t), newFakeHost()
	h := start(t, f, host, statePath(t))
	f.conn(t, 0)
	f.push(t, 2, "MESSAGE_CREATE", f.dmWith("1", "chan1", "42", "look at these", f.file("a.png", 10, 10), f.file("b.txt", 5, 5)))
	waitFor(t, "chat with attachments", func() bool { host.mu.Lock(); defer host.mu.Unlock(); return len(host.sentWith) == 1 })
	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.saved) != 2 || host.saved[0].name != "a.png" || len(host.saved[0].data) != 10 {
		t.Fatalf("saved = %+v", host.saved)
	}
	if sw := host.sentWith[0]; sw.text != "look at these" || fmt.Sprint(sw.ids) != "[att1 att2]" {
		t.Fatalf("sentWith = %+v", sw)
	}
	for _, d := range f.downloaded() {
		if d.auth != "" {
			t.Fatalf("bot token sent to the CDN for %s", d.name)
		}
	}
	if strings.Contains(h.logs.String(), "SIGNEDSECRET") || strings.Contains(h.logs.String(), testToken) {
		t.Fatalf("secret in logs:\n%s", h.logs.String())
	}
}

func TestOversizeAttachmentIsRejectedByDeclaredSizeAndStream(t *testing.T) {
	f, host := newFakeDiscord(t), newFakeHost()
	host.maxSize = 100
	start(t, f, host, statePath(t))
	f.conn(t, 0)
	f.push(t, 2, "MESSAGE_CREATE", f.dmWith("1", "chan1", "42", "",
		f.file("declared.bin", 5000, 5000), f.file("liar.bin", 10, 5000)))
	waitFor(t, "rejection notes", func() bool { return len(f.messages("chan1")) == 1 })
	note := f.messages("chan1")[0]
	if !strings.Contains(note, "declared.bin: That file is 1 MB; the limit is 1 MB.") {
		t.Fatalf("note = %q", note)
	}
	if !strings.Contains(note, "liar.bin") || !strings.Contains(note, "limit") {
		t.Fatalf("note = %q", note)
	}
	for _, d := range f.downloaded() {
		if d.name == "declared.bin" {
			t.Fatal("downloaded a file whose declared size is over the limit")
		}
	}
	if len(host.saved) != 0 || len(host.sentWith) != 0 {
		t.Fatal("oversize files were kept or sent")
	}
}

func TestExtraAttachmentsAreDroppedWithANote(t *testing.T) {
	f, host := newFakeDiscord(t), newFakeHost()
	host.maxFiles = 2
	start(t, f, host, statePath(t))
	f.conn(t, 0)
	f.push(t, 2, "MESSAGE_CREATE", f.dmWith("1", "chan1", "42", "three",
		f.file("1.txt", 1, 1), f.file("2.txt", 1, 1), f.file("3.txt", 1, 1)))
	waitFor(t, "chat sent", func() bool { host.mu.Lock(); defer host.mu.Unlock(); return len(host.sentWith) == 1 })
	if n := len(host.sentWith[0].ids); n != 2 {
		t.Fatalf("ids = %v, want 2", host.sentWith[0].ids)
	}
	if msgs := f.messages("chan1"); len(msgs) != 1 || !strings.Contains(msgs[0], "ignored the last 1") {
		t.Fatalf("messages = %q", msgs)
	}
}

func TestNotAllowedUsersAttachmentIsNeverDownloaded(t *testing.T) {
	f, host := newFakeDiscord(t), newFakeHost()
	h := start(t, f, host, statePath(t))
	f.conn(t, 0)
	f.push(t, 2, "MESSAGE_CREATE", f.dmWith("1", "chanX", "666", "", f.file("evil.bin", 5, 5)))
	f.push(t, 3, "MESSAGE_CREATE", f.dm("2", "chan1", "42", "sync"))
	h.waitSent(t, 1)
	if len(f.downloaded()) != 0 || len(host.saved) != 0 {
		t.Fatal("a stranger's file was fetched")
	}
}

func TestMessageWithoutTextOrFileGetsHelp(t *testing.T) {
	f, host := newFakeDiscord(t), newFakeHost()
	start(t, f, host, statePath(t))
	f.conn(t, 0)
	f.push(t, 2, "MESSAGE_CREATE", f.dm("1", "chan1", "42", ""))
	waitFor(t, "help", func() bool { return len(f.messages("chan1")) == 1 })
	if got := f.messages("chan1")[0]; strings.Contains(got, "Only text") || !strings.Contains(got, "attached files") {
		t.Fatalf("reply = %q", got)
	}
}
