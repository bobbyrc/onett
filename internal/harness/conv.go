package harness

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
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
// These processes are direct children of onett, not behind a run shim: a restart of onett ends
// them, and the conversation resumes its session in a new process on the next message.

// convs is the process-wide registry. Harnesses are built anew for each use, so it cannot live
// on one.
var convs = &convRegistry{procs: map[string]*convProc{}}

type convRegistry struct {
	mu    sync.Mutex
	procs map[string]*convProc
}

// ConvStart is how an adapter starts the process of a conversation.
type ConvStart struct {
	// Fingerprint identifies everything fixed when the process starts (system prompt, model,
	// arguments, environment). A turn whose fingerprint differs from the live process's replaces
	// the process.
	Fingerprint string
	// Encode turns a prompt into the bytes written to the process's stdin.
	Encode func(prompt string) ([]byte, error)
	// Start starts the process with first (an encoded prompt) as its first input, keeping stdin
	// open.
	Start func(first []byte) (*Proc, error)
}

// convProc is one live process and the turn it is answering, if any.
type convProc struct {
	key    string
	fp     string
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
	convs.mu.Lock()
	defer convs.mu.Unlock()

	// A live process can turn out to be gone only when the write fails; then one fresh start.
	for attempt := 0; attempt < 2; attempt++ {
		c := convs.procs[spec.ConversationID]
		if c != nil {
			t, err := c.beginTurn(spec, cs)
			switch {
			case err == nil:
				if werr := c.write(t, spec.Prompt); werr != nil {
					c.abandon(t)
					c.stop()
					delete(convs.procs, spec.ConversationID)
					continue
				}
				return t.watch(ctx), nil
			case errors.Is(err, errConvBusy):
				return nil, err
			}
			// Stopped or configured differently: replace it.
			c.stop()
			delete(convs.procs, spec.ConversationID)
		}
		first, err := cs.Encode(spec.Prompt)
		if err != nil {
			return nil, err
		}
		p, err := cs.Start(first)
		if err != nil {
			return nil, err
		}
		c = &convProc{key: spec.ConversationID, fp: cs.Fingerprint, proc: p, encode: cs.Encode}
		t := newConvTurn(c, spec.KeepAlive)
		c.turn = t // before the router runs, so the first events have a turn to go to
		convs.procs[spec.ConversationID] = c
		go c.route()
		return t.watch(ctx), nil
	}
	return nil, fmt.Errorf("harness: conversation %s: agent process would not take input", spec.ConversationID)
}

func newConvTurn(c *convProc, keepAlive time.Duration) *convTurn {
	return &convTurn{c: c, keepAlive: keepAlive, events: make(chan Event, procEventBuffer), done: make(chan struct{})}
}

// beginTurn claims the process for a turn. It fails with errConvBusy while a turn runs, and with
// another error when the process cannot be reused.
func (c *convProc) beginTurn(spec Spec, cs ConvStart) (*convTurn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.turn != nil:
		return nil, errConvBusy
	case c.stopping || c.fp != cs.Fingerprint:
		return nil, errors.New("process cannot be reused")
	}
	if c.idle != nil {
		c.idle.Stop()
		c.idle = nil
	}
	t := newConvTurn(c, spec.KeepAlive)
	c.turn = t
	return t, nil
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
	convs.mu.Lock()
	if convs.procs[c.key] == c {
		delete(convs.procs, c.key)
	}
	convs.mu.Unlock()
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

// StopConversations stops every live conversation process (at shutdown).
func StopConversations() {
	convs.mu.Lock()
	all := make([]*convProc, 0, len(convs.procs))
	for _, c := range convs.procs {
		all = append(all, c)
	}
	convs.mu.Unlock()
	for _, c := range all {
		c.stop()
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
func (t *convTurn) Cancel() {
	t.c.mu.Lock()
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
