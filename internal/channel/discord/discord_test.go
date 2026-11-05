package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/bobbyrc/onett/internal/attach"
	"github.com/bobbyrc/onett/internal/domain"
)

const testToken = "MTIz.SECRET-TOKEN.xyz"

func init() { backoffBase = time.Millisecond }

type download struct{ name, auth string }

type restCall struct {
	Method string
	Path   string
	Auth   string
	Body   map[string]any
}

// gwConn is one Gateway connection the fake accepted.
type gwConn struct {
	c      *websocket.Conn
	ctx    context.Context
	resume chan map[string]any // RESUME payloads received on this connection
}

// fakeDiscord serves both the REST API (under /api) and the Gateway (at /gateway).
type fakeDiscord struct {
	t         *testing.T
	srv       *httptest.Server
	interval  int // heartbeat_interval sent in HELLO, ms
	failPath  string
	cdnSizes  map[string]int // file name -> bytes served at /cdn/<name>
	downloads []download

	mu         sync.Mutex
	calls      []restCall
	nextMsg    int
	identifies []map[string]any
	heartbeats int
	conns      []*gwConn
	closeCode  websocket.StatusCode // when set, close the next connection with it right after HELLO
}

func newFakeDiscord(t *testing.T) *fakeDiscord {
	f := &fakeDiscord{t: t, interval: 20}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeDiscord) wsURL() string {
	return "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/gateway"
}

func (f *fakeDiscord) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/gateway" {
		f.gateway(w, r)
		return
	}
	if name, ok := strings.CutPrefix(r.URL.Path, "/cdn/"); ok {
		f.mu.Lock()
		f.downloads = append(f.downloads, download{name, r.Header.Get("Authorization")})
		size := f.cdnSizes[name]
		f.mu.Unlock()
		_, _ = w.Write(bytes.Repeat([]byte("x"), size))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api")
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.calls = append(f.calls, restCall{r.Method, path, r.Header.Get("Authorization"), body})
	f.nextMsg++
	id := f.nextMsg
	fail := f.failPath != "" && strings.HasPrefix(path, f.failPath)
	f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bot "+testToken && !strings.HasPrefix(path, "/interactions/") {
		http.Error(w, `{"message":"401: Unauthorized"}`, 401)
		return
	}
	if fail {
		w.WriteHeader(500)
		fmt.Fprintf(w, `{"message":"boom, token was %s"}`, testToken)
		return
	}
	switch {
	case path == "/gateway/bot":
		fmt.Fprintf(w, `{"url":%q}`, f.wsURL())
	case path == "/users/@me/channels":
		fmt.Fprintf(w, `{"id":"dm-%v"}`, body["recipient_id"])
	case strings.HasSuffix(path, "/messages") && r.Method == http.MethodPost:
		fmt.Fprintf(w, `{"id":"m%d"}`, id)
	case strings.HasSuffix(path, "/typing"), strings.HasSuffix(path, "/callback"):
		w.WriteHeader(204)
	default:
		fmt.Fprint(w, `{}`)
	}
}

func (f *fakeDiscord) gateway(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := r.Context()
	gc := &gwConn{c: c, ctx: ctx, resume: make(chan map[string]any, 4)}
	hello := fmt.Sprintf(`{"op":10,"d":{"heartbeat_interval":%d}}`, f.interval)
	if err := c.Write(ctx, websocket.MessageText, []byte(hello)); err != nil {
		return
	}
	f.mu.Lock()
	code := f.closeCode
	f.closeCode = 0
	f.mu.Unlock()
	if code != 0 {
		c.Close(code, "refused")
		return
	}
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var fr struct {
			Op int            `json:"op"`
			D  map[string]any `json:"d"`
		}
		_ = json.Unmarshal(data, &fr)
		switch fr.Op {
		case 1:
			f.mu.Lock()
			f.heartbeats++
			f.mu.Unlock()
			c.Write(ctx, websocket.MessageText, []byte(`{"op":11}`))
		case 2:
			f.mu.Lock()
			f.identifies = append(f.identifies, fr.D)
			f.conns = append(f.conns, gc)
			f.mu.Unlock()
			ready := fmt.Sprintf(`{"op":0,"s":1,"t":"READY","d":{"session_id":"sess","resume_gateway_url":%q,"user":{"id":"BOT"}}}`, f.wsURL())
			c.Write(ctx, websocket.MessageText, []byte(ready))
		case 6:
			f.mu.Lock()
			f.conns = append(f.conns, gc)
			f.mu.Unlock()
			gc.resume <- fr.D
			c.Write(ctx, websocket.MessageText, []byte(`{"op":0,"s":3,"t":"RESUMED","d":{}}`))
		}
	}
}

// push sends a dispatch event on the newest connection.
func (f *fakeDiscord) push(t *testing.T, seq int, typ string, d any) {
	t.Helper()
	gc := f.conn(t, 0)
	b, _ := json.Marshal(map[string]any{"op": 0, "s": seq, "t": typ, "d": d})
	if err := gc.c.Write(gc.ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

// conn returns the n-th connection that finished identifying/resuming; n<0 counts... newest is 0.
func (f *fakeDiscord) conn(t *testing.T, newest int) *gwConn {
	t.Helper()
	waitFor(t, "gateway connection", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.conns) > newest })
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conns[len(f.conns)-1-newest]
}

func (f *fakeDiscord) dm(id, channel, userID, content string) map[string]any {
	return map[string]any{"id": id, "channel_id": channel, "content": content,
		"author": map[string]any{"id": userID, "username": "u" + userID}}
}

func (f *fakeDiscord) rest(method, pathContains string) []restCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []restCall
	for _, c := range f.calls {
		if c.Method == method && strings.Contains(c.Path, pathContains) {
			out = append(out, c)
		}
	}
	return out
}

// messages returns the content of every message posted to a channel.
func (f *fakeDiscord) messages(channel string) []string {
	var out []string
	for _, c := range f.rest("POST", "/channels/"+channel+"/messages") {
		out = append(out, c.Body["content"].(string))
	}
	return out
}

type fakeHost struct {
	mu        sync.Mutex
	saved     []savedFile
	sentWith  []sentWith
	maxSize   int64 // 0 = 25 MB
	maxFiles  int   // 0 = 10
	convs     int
	sent      [][2]string // conversation, text
	decisions []string
	decideErr error
	events    chan domain.Event
	unsubbed  chan struct{}
}

func newFakeHost() *fakeHost {
	return &fakeHost{events: make(chan domain.Event, 16), unsubbed: make(chan struct{})}
}

func (h *fakeHost) CreateConversation(_ context.Context, title string) (domain.Conversation, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.convs++
	return domain.Conversation{ID: fmt.Sprintf("conv%d", h.convs), Title: title}, nil
}

func (h *fakeHost) SendChat(_ context.Context, conv, text string) (domain.ChatMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sent = append(h.sent, [2]string{conv, text})
	return domain.ChatMessage{ID: "u", ConversationID: conv, Role: "user", Text: text}, nil
}

func (h *fakeHost) SendChatWith(_ context.Context, conv, text string, ids []string) (domain.ChatMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sentWith = append(h.sentWith, sentWith{conv, text, ids})
	return domain.ChatMessage{ID: "u", ConversationID: conv, Role: "user", Text: text}, nil
}

func (h *fakeHost) MaxAttachmentSize() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.maxSize == 0 {
		return 25 << 20
	}
	return h.maxSize
}

func (h *fakeHost) MaxAttachmentsPerMessage() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.maxFiles == 0 {
		return 10
	}
	return h.maxFiles
}

func (h *fakeHost) SaveAttachment(_ context.Context, conv, name string, r io.Reader) (domain.Attachment, error) {
	limit := h.MaxAttachmentSize()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return domain.Attachment{}, err
	}
	if int64(len(data)) > limit {
		return domain.Attachment{}, attach.ErrTooLarge
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.saved = append(h.saved, savedFile{conv, name, data})
	return domain.Attachment{ID: fmt.Sprintf("att%d", len(h.saved)), ConversationID: conv, Name: name, MIME: "image/png", Size: int64(len(data))}, nil
}

func (h *fakeHost) DecideApproval(_ context.Context, id string, approve bool, note string) (domain.Approval, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.decideErr != nil {
		return domain.Approval{}, h.decideErr
	}
	h.decisions = append(h.decisions, fmt.Sprintf("%s/%v/%s", id, approve, note))
	return domain.Approval{ID: id}, nil
}

func (h *fakeHost) Subscribe() (<-chan domain.Event, func()) {
	return h.events, func() { close(h.unsubbed) }
}

func (h *fakeHost) emit(typ string, data any) {
	b, _ := json.Marshal(data)
	h.events <- domain.Event{Type: typ, Data: b}
}

func (h *fakeHost) snapshot() (sent [][2]string, decisions []string, convs int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([][2]string(nil), h.sent...), append([]string(nil), h.decisions...), h.convs
}

type savedFile struct {
	conv, name string
	data       []byte
}

type sentWith struct {
	conv, text string
	ids        []string
}

type harness struct {
	f      *fakeDiscord
	host   *fakeHost
	state  string
	logs   *syncBuf
	cancel context.CancelFunc
	done   chan error
	once   sync.Once
}

// start runs a bot allowed for the given users (default "42") against the fake Discord.
func start(t *testing.T, f *fakeDiscord, host *fakeHost, state string, users ...string) *harness {
	t.Helper()
	if len(users) == 0 {
		users = []string{"42"}
	}
	logs := &syncBuf{}
	b, err := New(Options{Token: testToken, AllowedUserIDs: users, StateFile: state, APIURL: f.srv.URL + "/api"},
		host, slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{f: f, host: host, state: state, logs: logs, cancel: cancel, done: make(chan error, 1)}
	go func() { h.done <- b.Run(ctx) }()
	t.Cleanup(h.stop)
	return h
}

func (h *harness) stop() {
	h.cancel()
	h.once.Do(func() {
		select {
		case <-h.done:
		case <-time.After(3 * time.Second):
		}
	})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func (h *harness) waitSent(t *testing.T, n int) {
	t.Helper()
	waitFor(t, "chat sent to host", func() bool { s, _, _ := h.host.snapshot(); return len(s) >= n })
}

// syncBuf is a log sink that is safe to read while the bot writes.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func statePath(t *testing.T) string { return filepath.Join(t.TempDir(), "channels", "discord.json") }

func TestIdentifyAndHeartbeat(t *testing.T) {
	f := newFakeDiscord(t)
	start(t, f, newFakeHost(), statePath(t))
	gc := f.conn(t, 0)
	_ = gc
	f.mu.Lock()
	id := f.identifies[0]
	f.mu.Unlock()
	if id["token"] != testToken {
		t.Fatalf("identify token = %v", id["token"])
	}
	if id["intents"] != float64(1<<12) {
		t.Fatalf("intents = %v, want DIRECT_MESSAGES only", id["intents"])
	}
	waitFor(t, "two heartbeats", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.heartbeats >= 2 })
}

func TestGatewayURLOverride(t *testing.T) {
	f := newFakeDiscord(t)
	b, err := New(Options{Token: testToken, AllowedUserIDs: []string{"42"}, StateFile: statePath(t),
		APIURL: f.srv.URL + "/api", GatewayURL: f.wsURL()}, newFakeHost(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()
	f.conn(t, 0)
	cancel()
	<-done
	if n := len(f.rest("GET", "/gateway/bot")); n != 0 {
		t.Fatalf("GET /gateway/bot called %d times despite override", n)
	}
}

func TestAllowedDMCreatesConversationAndPersists(t *testing.T) {
	f := newFakeDiscord(t)
	host := newFakeHost()
	state := statePath(t)
	h := start(t, f, host, state)
	f.push(t, 2, "MESSAGE_CREATE", f.dm("1", "chan1", "42", "hello"))
	h.waitSent(t, 1)
	sent, _, convs := host.snapshot()
	if convs != 1 || sent[0] != [2]string{"conv1", "hello"} {
		t.Fatalf("convs=%d sent=%v", convs, sent)
	}
	waitFor(t, "typing indicator", func() bool { return len(f.rest("POST", "/typing")) > 0 })
	h.stop()

	info, err := os.Stat(state)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %v", info.Mode().Perm())
	}
	data, _ := os.ReadFile(state)
	if strings.Contains(string(data), testToken) {
		t.Fatal("state file holds the token")
	}

	// A restarted bot reuses the conversation instead of creating another.
	f2, host2 := newFakeDiscord(t), newFakeHost()
	h2 := start(t, f2, host2, state)
	f2.push(t, 2, "MESSAGE_CREATE", f2.dm("2", "chan1", "42", "again"))
	h2.waitSent(t, 1)
	sent, _, convs = host2.snapshot()
	if convs != 0 || sent[0] != [2]string{"conv1", "again"} {
		t.Fatalf("after restart convs=%d sent=%v", convs, sent)
	}
}

func TestIgnoresOthersGuildAndSelf(t *testing.T) {
	f := newFakeDiscord(t)
	host := newFakeHost()
	h := start(t, f, host, statePath(t))
	f.push(t, 2, "MESSAGE_CREATE", f.dm("1", "chanX", "666", "secret instructions"))
	f.push(t, 3, "MESSAGE_CREATE", f.dm("2", "chanX", "666", "more secret instructions"))
	guild := f.dm("3", "guildchan", "42", "from a server")
	guild["guild_id"] = "g1"
	f.push(t, 4, "MESSAGE_CREATE", guild)
	f.push(t, 5, "MESSAGE_CREATE", f.dm("4", "chan1", "BOT", "my own echo"))
	botAuthor := f.dm("5", "chan1", "42", "from a bot")
	botAuthor["author"].(map[string]any)["bot"] = true
	f.push(t, 6, "MESSAGE_CREATE", botAuthor)
	f.push(t, 7, "MESSAGE_CREATE", f.dm("6", "chan1", "42", "real one"))
	h.waitSent(t, 1)
	sent, _, convs := host.snapshot()
	if len(sent) != 1 || sent[0][1] != "real one" || convs != 1 {
		t.Fatalf("sent=%v convs=%d", sent, convs)
	}
	logs := h.logs.String()
	if n := strings.Count(logs, "ignoring user not on the allowlist"); n != 1 {
		t.Fatalf("logged %d times, want once:\n%s", n, logs)
	}
	if strings.Contains(logs, "secret instructions") {
		t.Fatal("ignored message content was logged")
	}
	if got := f.messages("chanX"); len(got) != 0 {
		t.Fatalf("replied to a non-allowlisted user: %v", got)
	}
}

func TestRepliesRelayedAndSplit(t *testing.T) {
	f := newFakeDiscord(t)
	host := newFakeHost()
	h := start(t, f, host, statePath(t))
	f.push(t, 2, "MESSAGE_CREATE", f.dm("1", "chan1", "42", "hi"))
	h.waitSent(t, 1)

	long := strings.Repeat("a", 1500) + "\n" + strings.Repeat("b", 1500)
	host.emit(domain.EvChatMessage, domain.ChatMessage{ID: "r1", ConversationID: "conv1", Role: "assistant", Text: long})
	host.emit(domain.EvChatMessage, domain.ChatMessage{ID: "r1", ConversationID: "conv1", Role: "assistant", Text: long}) // duplicate
	host.emit(domain.EvChatMessage, domain.ChatMessage{ID: "r2", ConversationID: "other", Role: "assistant", Text: "not ours"})
	host.emit(domain.EvChatMessage, domain.ChatMessage{ID: "r3", ConversationID: "conv1", Role: "user", Text: "echo of mine"})
	host.emit(domain.EvChatMessage, domain.ChatMessage{ID: "r3b", ConversationID: "conv1", Role: "system", Kind: domain.ChatKindInternal, Text: "onett prompt"})
	host.emit(domain.EvChatMessage, domain.ChatMessage{ID: "r4", ConversationID: "conv1", Role: "assistant", Text: "last"})
	waitFor(t, "replies", func() bool { return len(f.messages("chan1")) >= 3 })
	got := f.messages("chan1")
	if len(got) != 3 || got[0] != strings.Repeat("a", 1500) || got[1] != strings.Repeat("b", 1500) || got[2] != "last" {
		t.Fatalf("messages = %d: %.20q", len(got), got)
	}
	for _, c := range f.rest("POST", "/chan1/messages") {
		if p, _ := c.Body["allowed_mentions"].(map[string]any); p == nil {
			t.Fatal("message sent without allowed_mentions")
		}
	}
}

// approvalClick builds an INTERACTION_CREATE for a button on message msgID in channel.
func approvalClick(userID, channel, msgID, customID string) map[string]any {
	return map[string]any{"id": "i1", "token": "itok", "type": 3, "channel_id": channel,
		"user":    map[string]any{"id": userID, "username": "bob"},
		"data":    map[string]any{"custom_id": customID, "component_type": 2},
		"message": map[string]any{"id": msgID, "channel_id": channel, "content": "Approval needed: Merge #1\nrisk"}}
}

func requestApproval(host *fakeHost) {
	host.emit(domain.EvApprovalRequested, domain.Approval{ID: "ap1", Title: "Merge #1", Reason: "risk"})
}

func TestApprovalButtonsAndClick(t *testing.T) {
	f := newFakeDiscord(t)
	host := newFakeHost()
	start(t, f, host, statePath(t), "42", "43")
	f.conn(t, 0)
	requestApproval(host)
	waitFor(t, "both approval messages", func() bool {
		return len(f.messages("dm-42")) == 1 && len(f.messages("dm-43")) == 1
	})
	call := f.rest("POST", "/channels/dm-42/messages")[0]
	if call.Auth != "Bot "+testToken {
		t.Fatalf("auth = %q", call.Auth)
	}
	row := call.Body["components"].([]any)[0].(map[string]any)["components"].([]any)
	if len(row) != 2 || row[0].(map[string]any)["custom_id"] != "a:ap1:y" || row[1].(map[string]any)["custom_id"] != "a:ap1:n" {
		t.Fatalf("components = %v", row)
	}
	if !strings.Contains(call.Body["content"].(string), "Merge #1") {
		t.Fatalf("content = %v", call.Body["content"])
	}

	// A user off the allowlist clicking is refused and decides nothing.
	f.push(t, 2, "INTERACTION_CREATE", approvalClick("666", "dm-42", "m1", "a:ap1:y"))
	waitFor(t, "refusal callback", func() bool { return len(f.rest("POST", "/callback")) == 1 })
	if _, dec, _ := host.snapshot(); len(dec) != 0 {
		t.Fatalf("decisions = %v", dec)
	}

	// The owner approves: decision recorded, clicked message rewritten via the callback, the
	// copy in the other DM edited.
	f.push(t, 3, "INTERACTION_CREATE", approvalClick("42", "dm-42", f.sentID("dm-42"), "a:ap1:y"))
	waitFor(t, "edit of other copy", func() bool { return len(f.rest("PATCH", "/channels/dm-43/messages/")) == 1 })
	_, dec, _ := host.snapshot()
	if len(dec) != 1 || dec[0] != "ap1/true/via Discord by bob" {
		t.Fatalf("decisions = %v", dec)
	}
	cb := f.rest("POST", "/callback")[1]
	if cb.Path != "/interactions/i1/itok/callback" || cb.Body["type"] != float64(7) {
		t.Fatalf("callback = %+v", cb)
	}
	data := cb.Body["data"].(map[string]any)
	if !strings.HasSuffix(data["content"].(string), "Approved") || len(data["components"].([]any)) != 0 {
		t.Fatalf("callback data = %v", data)
	}
	edit := f.rest("PATCH", "/channels/dm-43/messages/")[0]
	if !strings.HasSuffix(edit.Body["content"].(string), "Approved") || len(edit.Body["components"].([]any)) != 0 {
		t.Fatalf("edit = %v", edit.Body)
	}
	if n := len(f.rest("PATCH", "/channels/dm-42/")); n != 0 {
		t.Fatalf("clicked message edited via REST %d times; the callback already did it", n)
	}
}

// sentID returns the message ID the fake gave the bot's message in a channel.
func (f *fakeDiscord) sentID(channel string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return "m" + fmt.Sprint(f.firstMsgIndex(channel))
}

// firstMsgIndex finds the sequence number the fake assigned to the first message in channel.
// Only called with f.mu held.
func (f *fakeDiscord) firstMsgIndex(channel string) int {
	n := 0
	for _, c := range f.calls {
		n++
		if c.Method == "POST" && c.Path == "/channels/"+channel+"/messages" {
			return n
		}
	}
	return 0
}

func TestRejectClick(t *testing.T) {
	f := newFakeDiscord(t)
	host := newFakeHost()
	start(t, f, host, statePath(t))
	f.conn(t, 0)
	requestApproval(host)
	waitFor(t, "approval message", func() bool { return len(f.messages("dm-42")) == 1 })
	f.push(t, 2, "INTERACTION_CREATE", approvalClick("42", "dm-42", f.sentID("dm-42"), "a:ap1:n"))
	waitFor(t, "callback", func() bool { return len(f.rest("POST", "/callback")) == 1 })
	_, dec, _ := host.snapshot()
	if len(dec) != 1 || dec[0] != "ap1/false/via Discord by bob" {
		t.Fatalf("decisions = %v", dec)
	}
	data := f.rest("POST", "/callback")[0].Body["data"].(map[string]any)
	if !strings.HasSuffix(data["content"].(string), "Rejected") {
		t.Fatalf("data = %v", data)
	}
}

func TestDecideErrorShownToUser(t *testing.T) {
	f := newFakeDiscord(t)
	host := newFakeHost()
	host.decideErr = errors.New("already decided")
	start(t, f, host, statePath(t))
	f.conn(t, 0)
	f.push(t, 2, "INTERACTION_CREATE", approvalClick("42", "dm-42", "m1", "a:ap1:y"))
	waitFor(t, "error callback", func() bool { return len(f.rest("POST", "/callback")) == 1 })
	data := f.rest("POST", "/callback")[0].Body["data"].(map[string]any)
	if data["content"] != "already decided" || data["flags"] != float64(64) {
		t.Fatalf("data = %v", data)
	}
}

func TestApprovalDecidedElsewhereEditsMessages(t *testing.T) {
	f := newFakeDiscord(t)
	host := newFakeHost()
	start(t, f, host, statePath(t))
	f.conn(t, 0)
	requestApproval(host)
	waitFor(t, "approval message", func() bool { return len(f.messages("dm-42")) == 1 })
	host.emit(domain.EvApprovalDecided, domain.Approval{ID: "ap1", Status: domain.ApprovalRejected})
	waitFor(t, "edit", func() bool { return len(f.rest("PATCH", "/channels/dm-42/messages/")) == 1 })
	body := f.rest("PATCH", "/channels/dm-42/messages/")[0].Body
	if !strings.HasSuffix(body["content"].(string), "Rejected") || len(body["components"].([]any)) != 0 {
		t.Fatalf("edit = %v", body)
	}
}

func TestReconnectResumesAfterGatewayCloses(t *testing.T) {
	f := newFakeDiscord(t)
	host := newFakeHost()
	h := start(t, f, host, statePath(t))
	first := f.conn(t, 0)
	f.push(t, 2, "MESSAGE_CREATE", f.dm("1", "chan1", "42", "one"))
	h.waitSent(t, 1)
	first.c.Close(websocket.StatusGoingAway, "restart")

	// The new connection resumes the old session from the last sequence number.
	waitFor(t, "second connection", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.conns) == 2 })
	second := f.conn(t, 0)
	select {
	case r := <-second.resume:
		if r["session_id"] != "sess" || r["seq"] != float64(2) || r["token"] != testToken {
			t.Fatalf("resume = %v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no RESUME")
	}
	f.push(t, 4, "MESSAGE_CREATE", f.dm("2", "chan1", "42", "two"))
	h.waitSent(t, 2)
}

func TestInvalidSessionIdentifiesAgain(t *testing.T) {
	f := newFakeDiscord(t)
	h := start(t, f, newFakeHost(), statePath(t))
	first := f.conn(t, 0)
	first.c.Write(first.ctx, websocket.MessageText, []byte(`{"op":9,"d":false}`))
	waitFor(t, "second identify", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.identifies) == 2 })
	_ = h
}

func TestFatalCloseCodeEndsRun(t *testing.T) {
	f := newFakeDiscord(t)
	f.closeCode = 4004
	b, err := New(Options{Token: testToken, AllowedUserIDs: []string{"42"}, StateFile: statePath(t), APIURL: f.srv.URL + "/api"},
		newFakeHost(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- b.Run(context.Background()) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "4004") || strings.Contains(err.Error(), testToken) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop on a fatal close code")
	}
}

func TestRunReturnsNilOnCancelAndUnsubscribes(t *testing.T) {
	f := newFakeDiscord(t)
	host := newFakeHost()
	h := start(t, f, host, statePath(t))
	f.conn(t, 0)
	h.cancel()
	select {
	case err := <-h.done:
		h.once.Do(func() {}) // done is drained; stop() must not wait for it again
		if err != nil {
			t.Fatalf("Run = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop on cancel")
	}
	select {
	case <-host.unsubbed:
	default:
		t.Fatal("did not unsubscribe")
	}
}

func TestTokenNeverLogged(t *testing.T) {
	f := newFakeDiscord(t)
	f.failPath = "/channels/chan1/messages"
	host := newFakeHost()
	h := start(t, f, host, statePath(t))
	f.push(t, 2, "MESSAGE_CREATE", f.dm("1", "chan1", "42", "hi"))
	h.waitSent(t, 1)
	// The reply's send fails with a server message that echoes the token.
	host.emit(domain.EvChatMessage, domain.ChatMessage{ID: "r1", ConversationID: "conv1", Role: "assistant", Text: "x"})
	waitFor(t, "failed send logged", func() bool { return strings.Contains(h.logs.String(), "send message failed") })
	h.stop()
	logs := h.logs.String()
	if strings.Contains(logs, testToken) || strings.Contains(logs, "SECRET-TOKEN") {
		t.Fatalf("token leaked into logs:\n%s", logs)
	}
	if !strings.Contains(logs, "[redacted]") {
		t.Fatalf("expected the echoed token to be redacted:\n%s", logs)
	}
}

func TestTransportErrorDoesNotLeakToken(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // connection refused
	b, err := New(Options{Token: testToken, AllowedUserIDs: []string{"42"}, StateFile: statePath(t), APIURL: url + "/" + testToken},
		newFakeHost(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = b.call(context.Background(), http.MethodGet, "/gateway/bot", nil, nil)
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestSplitText(t *testing.T) {
	if got := splitText("short", 2000); len(got) != 1 || got[0] != "short" {
		t.Fatalf("got %v", got)
	}
	long := strings.Repeat("é", 4500)
	got := splitText(long, 2000)
	if len(got) != 3 {
		t.Fatalf("got %d pieces", len(got))
	}
	for _, p := range got {
		if n := len([]rune(p)); n > 2000 {
			t.Fatalf("piece of %d runes", n)
		}
	}
	if strings.Join(got, "") != long {
		t.Fatal("pieces do not rejoin")
	}
}

func TestNewValidatesOptions(t *testing.T) {
	ok := Options{Token: "t", AllowedUserIDs: []string{"1"}, StateFile: filepath.Join(t.TempDir(), "s.json")}
	host := newFakeHost()
	for name, mutate := range map[string]func(*Options){
		"no token":     func(o *Options) { o.Token = "" },
		"no allowlist": func(o *Options) { o.AllowedUserIDs = nil },
		"empty id":     func(o *Options) { o.AllowedUserIDs = []string{""} },
		"no state":     func(o *Options) { o.StateFile = "" },
	} {
		o := ok
		mutate(&o)
		if _, err := New(o, host, nil); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := New(ok, nil, nil); err == nil {
		t.Error("nil host: want error")
	}
	if _, err := New(ok, host, nil); err != nil {
		t.Errorf("valid options: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte("{"), 0o600)
	o := ok
	o.StateFile = bad
	if _, err := New(o, host, nil); err == nil {
		t.Error("corrupt state file: want error")
	}
}
