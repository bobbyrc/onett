package telegram

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

	"github.com/bobbyrc/onett/internal/domain"
)

const testToken = "123456:SECRET-TOKEN"

func init() { backoffBase = time.Millisecond }

type apiCall struct {
	Method string
	Body   map[string]any
}

// fakeAPI is a scripted Bot API: push queues updates for getUpdates, every other method is
// recorded.
type fakeAPI struct {
	srv     *httptest.Server
	mu      sync.Mutex
	queue   [][]string
	calls   []apiCall
	nextMsg int64
	nextUp  int64
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{nextMsg: 100}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/bot"+testToken+"/") {
		http.Error(w, "bad token", 404)
		return
	}
	method := strings.TrimPrefix(r.URL.Path, "/bot"+testToken+"/")
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.calls = append(f.calls, apiCall{method, body})
	var batch []string
	if method == "getUpdates" && len(f.queue) > 0 {
		batch, f.queue = f.queue[0], f.queue[1:]
	}
	var id int64
	if method == "sendMessage" {
		f.nextMsg++
		id = f.nextMsg
	}
	f.mu.Unlock()
	switch method {
	case "getUpdates":
		if batch == nil {
			select {
			case <-r.Context().Done():
			case <-time.After(3 * time.Millisecond):
			}
		}
		fmt.Fprintf(w, `{"ok":true,"result":[%s]}`, strings.Join(batch, ","))
	case "sendMessage":
		fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d}}`, id)
	default:
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	}
}

func (f *fakeAPI) push(updates ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = append(f.queue, updates)
}

func (f *fakeAPI) msg(userID, chatID int64, chatType, text string) string {
	f.mu.Lock()
	f.nextUp++
	id := f.nextUp
	f.mu.Unlock()
	b, _ := json.Marshal(text)
	return fmt.Sprintf(`{"update_id":%d,"message":{"message_id":1,"from":{"id":%d},"chat":{"id":%d,"type":%q},"text":%s}}`,
		id, userID, chatID, chatType, b)
}

func (f *fakeAPI) cb(userID int64, data string) string {
	f.mu.Lock()
	f.nextUp++
	id := f.nextUp
	f.mu.Unlock()
	return fmt.Sprintf(`{"update_id":%d,"callback_query":{"id":"cb%d","from":{"id":%d,"username":"bob"},"data":%q}}`, id, id, userID, data)
}

func (f *fakeAPI) byMethod(m string) []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []apiCall
	for _, c := range f.calls {
		if c.Method == m {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeAPI) texts() []string {
	var out []string
	for _, c := range f.byMethod("sendMessage") {
		out = append(out, c.Body["text"].(string))
	}
	return out
}

type fakeHost struct {
	mu        sync.Mutex
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

type harness struct {
	api    *fakeAPI
	host   *fakeHost
	state  string
	logs   *syncBuf
	cancel context.CancelFunc
	done   chan error
	once   sync.Once
}

// start runs a bot allowed for user 42 against the fake API.
func start(t *testing.T, api *fakeAPI, host *fakeHost, state string) *harness {
	t.Helper()
	logs := &syncBuf{}
	b, err := New(Options{Token: testToken, AllowedUserIDs: []int64{42}, StateFile: state, APIURL: api.srv.URL},
		host, slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{api: api, host: host, state: state, logs: logs, cancel: cancel, done: make(chan error, 1)}
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

func TestAllowlistIgnoresOthers(t *testing.T) {
	api, host := newFakeAPI(t), newFakeHost()
	api.push(
		api.msg(7, 7, "private", "stranger"),
		api.msg(42, -100, "group", "from group"),
		api.cb(7, "a:ap1:y"),
		api.msg(42, 42, "private", "hello"),
	)
	h := start(t, api, host, filepath.Join(t.TempDir(), "s.json"))
	h.waitSent(t, 1)
	sent, decisions, convs := host.snapshot()
	if len(sent) != 1 || sent[0][1] != "hello" || convs != 1 || len(decisions) != 0 {
		t.Fatalf("sent=%v decisions=%v convs=%d", sent, decisions, convs)
	}
	if strings.Contains(h.logs.String(), "stranger") {
		t.Fatal("ignored message text was logged")
	}
}

func TestConversationReusedAcrossRestart(t *testing.T) {
	state := filepath.Join(t.TempDir(), "sub", "s.json")
	api, host := newFakeAPI(t), newFakeHost()
	api.push(api.msg(42, 42, "private", "one"), api.msg(42, 42, "private", "two"))
	h := start(t, api, host, state)
	h.waitSent(t, 2)
	waitFor(t, "typing action", func() bool { return len(api.byMethod("sendChatAction")) == 2 })
	h.stop()
	sent, _, convs := host.snapshot()
	if convs != 1 || sent[0][0] != "conv1" || sent[1][0] != "conv1" {
		t.Fatalf("sent=%v convs=%d", sent, convs)
	}
	if fi, err := os.Stat(state); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("state file: %v %v", fi, err)
	}
	if fi, _ := os.Stat(filepath.Dir(state)); fi.Mode().Perm() != 0o700 {
		t.Fatalf("state dir mode %v", fi.Mode())
	}
	if raw, _ := os.ReadFile(state); strings.Contains(string(raw), "SECRET") {
		t.Fatal("token in state file")
	}

	api2, host2 := newFakeAPI(t), newFakeHost()
	api2.nextUp = 10
	api2.push(api2.msg(42, 42, "private", "three"))
	h2 := start(t, api2, host2, state)
	h2.waitSent(t, 1)
	sent, _, convs = host2.snapshot()
	if convs != 0 || sent[0][0] != "conv1" {
		t.Fatalf("after restart sent=%v convs=%d", sent, convs)
	}
	first := api2.byMethod("getUpdates")[0]
	if first.Body["offset"].(float64) != 3 { // two updates handled before the restart
		t.Fatalf("offset after restart = %v", first.Body["offset"])
	}
}

func TestNewStartsFreshConversation(t *testing.T) {
	api, host := newFakeAPI(t), newFakeHost()
	api.push(api.msg(42, 42, "private", "a"), api.msg(42, 42, "private", "/new"), api.msg(42, 42, "private", "b"))
	h := start(t, api, host, filepath.Join(t.TempDir(), "s.json"))
	h.waitSent(t, 2)
	sent, _, convs := host.snapshot()
	if convs != 2 || sent[0][0] != "conv1" || sent[1][0] != "conv2" {
		t.Fatalf("sent=%v convs=%d", sent, convs)
	}
	if got := api.texts(); len(got) != 1 || got[0] != "Started a new conversation." {
		t.Fatalf("replies = %q", got)
	}
}

func TestRepliesRoutedDedupedAndSplit(t *testing.T) {
	api, host := newFakeAPI(t), newFakeHost()
	api.push(api.msg(42, 42, "private", "hi"))
	h := start(t, api, host, filepath.Join(t.TempDir(), "s.json"))
	h.waitSent(t, 1)

	host.emit(domain.EvChatMessage, domain.ChatMessage{ID: "m0", ConversationID: "conv1", Role: "user", Text: "echo"})
	host.emit(domain.EvChatMessage, domain.ChatMessage{ID: "m1", ConversationID: "other", Role: "assistant", Text: "elsewhere"})
	host.emit(domain.EvChatMessage, domain.ChatMessage{ID: "m2", ConversationID: "conv1", Role: "assistant", Text: "hello back"})
	host.emit(domain.EvChatMessage, domain.ChatMessage{ID: "m2", ConversationID: "conv1", Role: "assistant", Text: "hello back"})
	host.emit(domain.EvChatMessage, domain.ChatMessage{ID: "m2b", ConversationID: "conv1", Role: "system", Kind: domain.ChatKindInternal, Text: "onett prompt"})
	long := strings.Repeat("é", 3000) + "\n" + strings.Repeat("ü", 3000)
	host.emit(domain.EvChatMessage, domain.ChatMessage{ID: "m3", ConversationID: "conv1", Role: "system", Text: long})
	waitFor(t, "replies", func() bool { return len(api.texts()) >= 3 })
	time.Sleep(10 * time.Millisecond) // let a wrongly sent duplicate show up
	got := api.texts()
	if len(got) != 3 || got[0] != "hello back" {
		t.Fatalf("texts = %d %q", len(got), got[0])
	}
	if got[1] != strings.Repeat("é", 3000) || got[2] != strings.Repeat("ü", 3000) {
		t.Fatal("long message not split on the newline")
	}
	for _, c := range api.byMethod("sendMessage") {
		if c.Body["chat_id"].(float64) != 42 || c.Body["parse_mode"] != nil {
			t.Fatalf("bad send %v", c.Body)
		}
	}
	for _, part := range splitText(strings.Repeat("x", 9000), maxText) {
		if len([]rune(part)) > maxText {
			t.Fatal("part over limit")
		}
	}
}

func approval(id, status string) domain.Approval {
	return domain.Approval{ID: id, Title: "Merge #5", Reason: "medium risk", Status: domain.ApprovalStatus(status)}
}

func TestApprovalCallbackDecidesAndEdits(t *testing.T) {
	api, host := newFakeAPI(t), newFakeHost()
	start(t, api, host, filepath.Join(t.TempDir(), "s.json"))
	host.emit(domain.EvApprovalRequested, approval("ap1", "pending"))
	waitFor(t, "approval message", func() bool { return len(api.texts()) == 1 })
	sm := api.byMethod("sendMessage")[0].Body
	if sm["chat_id"].(float64) != 42 || sm["text"] != "Approval needed: Merge #5\nmedium risk" {
		t.Fatalf("approval message %v", sm)
	}
	row := sm["reply_markup"].(map[string]any)["inline_keyboard"].([]any)[0].([]any)
	if row[0].(map[string]any)["callback_data"] != "a:ap1:y" || row[1].(map[string]any)["callback_data"] != "a:ap1:n" {
		t.Fatalf("keyboard %v", row)
	}

	api.push(api.cb(42, "a:ap1:y"))
	waitFor(t, "edit", func() bool { return len(api.byMethod("editMessageText")) == 1 })
	_, decisions, _ := host.snapshot()
	if len(decisions) != 1 || decisions[0] != "ap1/true/via Telegram by bob" {
		t.Fatalf("decisions %v", decisions)
	}
	if a := api.byMethod("answerCallbackQuery")[0].Body; a["text"] != "Approved" {
		t.Fatalf("answer %v", a)
	}
	e := api.byMethod("editMessageText")[0].Body
	if e["message_id"].(float64) != 101 || !strings.HasSuffix(e["text"].(string), "\n\nApproved") || e["reply_markup"] != nil {
		t.Fatalf("edit %v", e)
	}

	// The decision event that follows must not edit the message a second time.
	host.emit(domain.EvApprovalDecided, approval("ap1", "approved"))
	time.Sleep(10 * time.Millisecond)
	if n := len(api.byMethod("editMessageText")); n != 1 {
		t.Fatalf("%d edits", n)
	}
}

func TestDecideErrorShownToUser(t *testing.T) {
	api, host := newFakeAPI(t), newFakeHost()
	host.decideErr = errors.New("approval is already approved")
	start(t, api, host, filepath.Join(t.TempDir(), "s.json"))
	api.push(api.cb(42, "a:ap1:n"))
	waitFor(t, "answer", func() bool { return len(api.byMethod("answerCallbackQuery")) == 1 })
	if a := api.byMethod("answerCallbackQuery")[0].Body; a["text"] != "approval is already approved" {
		t.Fatalf("answer %v", a)
	}
	if len(api.byMethod("editMessageText")) != 0 {
		t.Fatal("message edited despite error")
	}
}

func TestApprovalDecidedElsewhereEditsMessage(t *testing.T) {
	api, host := newFakeAPI(t), newFakeHost()
	start(t, api, host, filepath.Join(t.TempDir(), "s.json"))
	host.emit(domain.EvApprovalRequested, approval("ap2", "pending"))
	waitFor(t, "approval message", func() bool { return len(api.texts()) == 1 })
	host.emit(domain.EvApprovalDecided, approval("unknown", "approved")) // ignored
	host.emit(domain.EvApprovalDecided, approval("ap2", "rejected"))
	waitFor(t, "edit", func() bool { return len(api.byMethod("editMessageText")) == 1 })
	e := api.byMethod("editMessageText")[0].Body
	if !strings.HasSuffix(e["text"].(string), "\n\nRejected") {
		t.Fatalf("edit %v", e)
	}
}

func TestTransportErrorDoesNotLeakToken(t *testing.T) {
	api := newFakeAPI(t)
	api.srv.Close() // connection refused
	b, err := New(Options{Token: testToken, AllowedUserIDs: []int64{42}, StateFile: filepath.Join(t.TempDir(), "s.json"), APIURL: api.srv.URL},
		newFakeHost(), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	err = b.call(context.Background(), "getUpdates", map[string]any{}, nil)
	if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "/bot") {
		t.Fatalf("error = %v", err)
	}

	// The same through Run: the retry log must be clean, and the loop keeps retrying.
	logs := &syncBuf{}
	b.log = slog.New(slog.NewTextHandler(logs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()
	waitFor(t, "retry logs", func() bool { return strings.Count(logs.String(), "getUpdates failed") >= 2 })
	cancel()
	<-done
	if strings.Contains(logs.String(), "SECRET") {
		t.Fatalf("token in logs: %s", logs)
	}
}

func TestRunReturnsNilOnCancelAndUnsubscribes(t *testing.T) {
	api, host := newFakeAPI(t), newFakeHost()
	h := start(t, api, host, filepath.Join(t.TempDir(), "s.json"))
	waitFor(t, "first poll", func() bool { return len(api.byMethod("getUpdates")) > 0 })
	h.cancel()
	h.once.Do(func() {}) // this test reads done itself
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatalf("Run = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}
	select {
	case <-host.unsubbed:
	default:
		t.Fatal("not unsubscribed")
	}
}

func TestNewValidatesOptions(t *testing.T) {
	ok := Options{Token: "t", AllowedUserIDs: []int64{1}, StateFile: "/tmp/x"}
	for name, mut := range map[string]func(*Options){
		"token": func(o *Options) { o.Token = "" }, "users": func(o *Options) { o.AllowedUserIDs = nil },
		"state": func(o *Options) { o.StateFile = "" },
	} {
		o := ok
		mut(&o)
		if _, err := New(o, newFakeHost(), nil); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}
