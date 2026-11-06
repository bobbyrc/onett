package harness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bobbyrc/onett/internal/runshim"
)

// Long-lived processes for conversations.
//
// Starting a fresh agent process for every message of a chat makes the agent re-read its whole
// prompt each time: its MCP servers connect again, its tool list is rebuilt and the prompt cache
// has to be refilled. An adapter whose CLI takes more input on stdin after each answer (claude
// -p with stream-json input) can instead keep one process per conversation. StartConvTurn is the
// shared part of that: the registry of live processes, the router that cuts their output into
// turns, and the idle timer. The adapter supplies how to start the process and how to encode a
// prompt, and marks the end of each turn with an Event whose TurnResult is set.
//
// Without limits.run_shim these processes are direct children of onett: a restart of onett ends
// them, and the conversation resumes its session in a new process on the next message. With it
// each process runs behind its own run shim (package runshim), named by ConvRunID, so it outlives
// a restart. The next turn after the restart finds the shim, replays its log to rebuild the
// parser's state and goes on with the same process, if it was idle between turns and was started
// with the same configuration; otherwise the old process is stopped and a new one resumes the
// session.

// convs is the process-wide registry. Harnesses are built anew for each use, so it cannot live
// on one.
var convs = &convRegistry{procs: map[string]*convProc{}, starts: map[string]*startLock{}}

type convRegistry struct {
	mu     sync.Mutex // guards the maps only; never held across a process start or a write
	procs  map[string]*convProc
	starts map[string]*startLock
}

// startLock serialises the starts and first writes of one conversation, so a slow spawn or a
// blocked stdin write of one conversation cannot hold up the others.
type startLock struct {
	mu   sync.Mutex
	refs int
}

// lockStart takes the conversation's start lock and returns the function that releases it.
func (r *convRegistry) lockStart(key string) func() {
	r.mu.Lock()
	l := r.starts[key]
	if l == nil {
		l = &startLock{}
		r.starts[key] = l
	}
	l.refs++
	r.mu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		r.mu.Lock()
		if l.refs--; l.refs == 0 {
			delete(r.starts, key)
		}
		r.mu.Unlock()
	}
}

func (r *convRegistry) get(key string) *convProc {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.procs[key]
}

// remove drops c from the registry unless a newer process already replaced it.
func (r *convRegistry) remove(key string, c *convProc) {
	r.mu.Lock()
	if r.procs[key] == c {
		delete(r.procs, key)
	}
	r.mu.Unlock()
}

// ConvStart is how an adapter starts the process of a conversation.
type ConvStart struct {
	// Fingerprint identifies everything fixed when the process starts (system prompt, model,
	// arguments, environment). A turn whose fingerprint differs from the live process's replaces
	// the process.
	Fingerprint string
	// Live identifies the settings that can change while the process runs (the model, the
	// permission mode). A turn whose Live differs from the process's calls Retune instead of
	// replacing the process, which keeps its prompt cache.
	Live string
	// Retune returns the input that moves a process whose Live was prev to this turn's Live, or
	// false when that cannot be done in place and the process must be replaced. Nil means every
	// change needs a new process.
	Retune func(prev string) ([]byte, bool)
	// Encode turns a prompt into the bytes written to the process's stdin.
	Encode func(prompt string) ([]byte, error)
	// Start starts the process with first (an encoded prompt) as its first input, keeping stdin
	// open.
	Start func(first []byte) (*Proc, error)

	// The rest is for a process behind a run shim; leave it zero for a direct child.
	//
	// RunDir is the shim's directory (runshim.RunDir of ConvRunID).
	RunDir string
	// Replay parses the shim's whole stream.log with a fresh parser.
	Replay func() ([]Event, error)
	// Reattach dials the shim and follows its output with a fresh parser, delivering events for
	// lines from stream offset from on only.
	Reattach func(from int64) (*Proc, error)
}

// ConvRunID is the run shim name of a conversation's process. It is stable, so a restarted onett
// finds the shim again.
func ConvRunID(conversationID string) string {
	return "conv-" + strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '_'
	}, conversationID)
}

// convFingerprintFile records the fingerprint a shim-backed process started with.
const convFingerprintFile = "conv.fp"

// convLiveFile records the Live settings of a shim-backed process, which change as it is retuned.
const convLiveFile = "conv.live"

// convProc is one live process and the turn it is answering, if any.
type convProc struct {
	key    string
	fp     string
	live   string // guarded by mu
	proc   *Proc
	encode func(string) ([]byte, error)

	mu       sync.Mutex
	turn     *convTurn
	stopping bool // stdin is closed or about to be; no new turn may use the process
	idle     *time.Timer
}

// convTurn is the Session of one turn.
type convTurn struct {
	c         *convProc
	keepAlive time.Duration
	events    chan Event
	done      chan struct{}
	res       Result
}

var (
	_ Session   = (*convTurn)(nil)
	_ Steerable = (*convTurn)(nil)
)

// errConvBusy is returned when a conversation's process is still answering an earlier turn.
var errConvBusy = errors.New("harness: conversation already has a turn running")

// StartConvTurn runs one turn of spec.ConversationID on its long-lived process, starting the
// process (or replacing a stopped or differently configured one) when needed. ctx ends the turn
// by cancelling the process, not just detaching from it.
func StartConvTurn(ctx context.Context, spec Spec, cs ConvStart) (Session, error) {
	if spec.ConversationID == "" || spec.KeepAlive <= 0 {
		return nil, errors.New("harness: StartConvTurn needs a conversation ID and a keep-alive")
	}
	defer convs.lockStart(spec.ConversationID)()

	// A live process can turn out to be gone only when the write fails; then one fresh start.
	// The first pass may also adopt a process an earlier onett left running behind its shim.
	for attempt := 0; attempt < 3; attempt++ {
		c := convs.get(spec.ConversationID)
		if c == nil && attempt == 0 && cs.Reattach != nil {
			if c = reattachConv(spec, cs); c != nil {
				convs.mu.Lock()
				convs.procs[spec.ConversationID] = c
				convs.mu.Unlock()
				go c.route()
			}
		}
		if c != nil {
			t, pre, err := c.beginTurn(spec, cs)
			switch {
			case err == nil:
				werr := error(nil)
				if len(pre) > 0 {
					werr = c.proc.WriteStdin(pre)
				}
				if werr == nil {
					werr = c.write(t, spec.Prompt)
				}
				if werr != nil {
					c.abandon(t)
					c.stop()
					convs.remove(spec.ConversationID, c)
					continue
				}
				if len(pre) > 0 && cs.RunDir != "" {
					_ = os.WriteFile(cs.RunDir+"/"+convLiveFile, []byte(cs.Live), 0o600)
				}
				return t.watch(ctx), nil
			case errors.Is(err, errConvBusy):
				return nil, err
			}
			// Stopped or configured differently: replace it.
			c.stop()
			convs.remove(spec.ConversationID, c)
		}
		first, err := cs.Encode(spec.Prompt)
		if err != nil {
			return nil, err
		}
		p, err := cs.Start(first)
		if err != nil {
			return nil, err
		}
		if cs.RunDir != "" {
			// Best effort: without it a restarted onett will not reuse this process.
			_ = os.WriteFile(cs.RunDir+"/"+convFingerprintFile, []byte(cs.Fingerprint), 0o600)
			_ = os.WriteFile(cs.RunDir+"/"+convLiveFile, []byte(cs.Live), 0o600)
		}
		c = &convProc{key: spec.ConversationID, fp: cs.Fingerprint, live: cs.Live, proc: p, encode: cs.Encode}
		t := newConvTurn(c, spec.KeepAlive)
		c.turn = t // before the router runs, so the first events have a turn to go to
		convs.mu.Lock()
		convs.procs[spec.ConversationID] = c
		convs.mu.Unlock()
		go c.route()
		return t.watch(ctx), nil
	}
	return nil, fmt.Errorf("harness: conversation %s: agent process would not take input", spec.ConversationID)
}

// reattachConv adopts the process an earlier onett left behind this conversation's shim, or
// returns nil when there is none worth keeping. A process that is not idle between turns (it was
// answering when onett went away: its next result would be taken for the next turn's), or that
// was started with other settings, is stopped and its shim waited for, so that a new one can take
// the same files and socket.
func reattachConv(spec Spec, cs ConvStart) *convProc {
	st, err := runshim.ReadStatus(cs.RunDir)
	if err != nil {
		return nil // no shim was ever started
	}
	// Anything unreadable below counts as "not reusable", so that a shim still holding the
	// socket is stopped rather than left to block the new one.
	fp, _ := os.ReadFile(cs.RunDir + "/" + convFingerprintFile)
	// One size for both the idle check and the reattach point: a shim still writing must not
	// move the line between "already seen" and "for the next turn".
	fi, statErr := os.Stat(runshim.StreamPath(cs.RunDir))
	if statErr != nil {
		return nil
	}
	size := fi.Size()
	evs, replayErr := cs.Replay()
	for len(evs) > 0 && evs[len(evs)-1].Offset > size {
		evs = evs[:len(evs)-1] // written after the snapshot
	}
	p, err := cs.Reattach(size)
	if err != nil {
		return nil // the shim is gone
	}
	idle := !st.Exited && replayErr == nil && len(evs) > 0 &&
		evs[len(evs)-1].TurnResult != nil && evs[len(evs)-1].Offset == size
	if string(fp) != cs.Fingerprint || !idle {
		p.Cancel()
		_, _ = p.Wait()
		for deadline := time.Now().Add(5 * time.Second); st.Socket != "" && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(st.Socket); err != nil {
				break
			}
		}
		return nil
	}
	live, _ := os.ReadFile(cs.RunDir + "/" + convLiveFile)
	c := &convProc{key: spec.ConversationID, fp: cs.Fingerprint, live: string(live), proc: p, encode: cs.Encode}
	c.idle = time.AfterFunc(spec.KeepAlive, c.stopIfIdle)
	return c
}

func newConvTurn(c *convProc, keepAlive time.Duration) *convTurn {
	return &convTurn{c: c, keepAlive: keepAlive, events: make(chan Event, procEventBuffer), done: make(chan struct{})}
}

// beginTurn claims the process for a turn. It fails with errConvBusy while a turn runs, and with
// another error when the process cannot be reused.
func (c *convProc) beginTurn(spec Spec, cs ConvStart) (*convTurn, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.turn != nil:
		return nil, nil, errConvBusy
	case c.stopping || c.fp != cs.Fingerprint:
		return nil, nil, errors.New("process cannot be reused")
	}
	// Settings that can change in place are sent ahead of the prompt, on the same stdin, so the
	// process applies them before it reads the message.
	var pre []byte
	if c.live != cs.Live {
		ok := false
		if cs.Retune != nil {
			pre, ok = cs.Retune(c.live)
		}
		if !ok {
			return nil, nil, errors.New("process cannot be retuned")
		}
		c.live = cs.Live
	}
	if c.idle != nil {
		c.idle.Stop()
		c.idle = nil
	}
	t := newConvTurn(c, spec.KeepAlive)
	c.turn = t
	return t, pre, nil
}

func (c *convProc) write(t *convTurn, prompt string) error {
	line, err := c.encode(prompt)
	if err != nil {
		return err
	}
	return c.proc.WriteStdin(line)
}

// abandon takes back a turn that never reached the process.
func (c *convProc) abandon(t *convTurn) {
	c.mu.Lock()
	if c.turn == t {
		c.turn = nil
	}
	c.mu.Unlock()
}

// watch cancels the turn's process when ctx ends before the turn does.
func (t *convTurn) watch(ctx context.Context) *convTurn {
	if ctx != nil {
		go func() {
			select {
			case <-ctx.Done():
				t.Cancel()
			case <-t.done:
			}
		}()
	}
	return t
}

// route hands the process's output to the turn in progress and ends the turn at its result.
// When the process exits it ends the turn in progress as failed and leaves the registry.
func (c *convProc) route() {
	for ev := range c.proc.Events() {
		c.mu.Lock()
		t := c.turn
		c.mu.Unlock()
		if ev.TurnResult != nil {
			if t != nil {
				c.endTurn(t, *ev.TurnResult)
			}
			continue
		}
		// Output outside a turn (stderr between turns) has nowhere to go.
		if t != nil {
			t.events <- ev
		}
	}
	res, _ := c.proc.Wait()
	c.mu.Lock()
	t := c.turn
	c.stopping = true
	if c.idle != nil {
		c.idle.Stop()
	}
	c.mu.Unlock()
	convs.remove(c.key, c)
	if t != nil {
		// The process ended before the turn's result: the turn failed, whatever the exit code.
		res.IsError = true
		if res.FinalText == "" {
			res.FinalText = "the agent process ended before the turn finished"
		}
		c.endTurn(t, res)
	}
}

// endTurn delivers the turn's result and starts the idle timer.
func (c *convProc) endTurn(t *convTurn, res Result) {
	c.mu.Lock()
	if c.turn == t {
		c.turn = nil
		if !c.stopping {
			c.idle = time.AfterFunc(t.keepAlive, c.stopIfIdle)
		}
	}
	c.mu.Unlock()
	t.res = res
	close(t.events)
	close(t.done)
}

// stopIfIdle is the idle timer: it closes the process's stdin unless a turn took it meanwhile.
func (c *convProc) stopIfIdle() {
	c.mu.Lock()
	idle := c.turn == nil && !c.stopping
	c.mu.Unlock()
	if idle {
		c.stop()
	}
}

// stop asks the process to exit by closing its stdin, which the agent CLI treats as the end of
// the conversation, and kills it if it has not exited after the usual grace period.
func (c *convProc) stop() {
	c.mu.Lock()
	if c.stopping {
		c.mu.Unlock()
		return
	}
	c.stopping = true
	c.mu.Unlock()
	c.proc.CloseStdin()
	time.AfterFunc(procKillGrace, c.proc.Cancel)
}

// StopConversations stops every live conversation process (at shutdown). One behind a run shim
// is let go instead, to be adopted by the next onett.
func StopConversations() {
	convs.mu.Lock()
	all := make([]*convProc, 0, len(convs.procs))
	for _, c := range convs.procs {
		all = append(all, c)
	}
	convs.mu.Unlock()
	for _, c := range all {
		if !c.proc.Detach() {
			c.stop()
			continue
		}
		// Forget it now: route notices the closed stream only a moment later, and a turn
		// started in between would write to a process nobody listens to.
		c.mu.Lock()
		c.stopping = true
		if c.idle != nil {
			c.idle.Stop()
		}
		c.mu.Unlock()
		convs.remove(c.key, c)
	}
}

// Events is closed when the turn's result arrives or the process exits.
func (t *convTurn) Events() <-chan Event { return t.events }

// Wait returns the turn's result. A process that exits mid-turn gives an IsError result.
func (t *convTurn) Wait() (Result, error) {
	<-t.done
	return t.res, nil
}

// Cancel kills the process: after a denial, a stall or a timeout its state is not trusted, and
// the conversation resumes its session in a new one. The turn ends when the process has exited.
// A Cancel that arrives after the turn ended (a context cancelled right after the result) does
// nothing: the process is idle or already serving the next turn.
func (t *convTurn) Cancel() {
	t.c.mu.Lock()
	if t.c.turn != t {
		t.c.mu.Unlock()
		return
	}
	t.c.stopping = true
	t.c.mu.Unlock()
	t.c.proc.Cancel()
}

// Send writes another message to the process during the turn. ErrInputClosed once the turn ended.
func (t *convTurn) Send(text string) error {
	select {
	case <-t.done:
		return ErrInputClosed
	default:
	}
	line, err := t.c.encode(text)
	if err != nil {
		return err
	}
	return t.c.proc.WriteStdin(line)
}
