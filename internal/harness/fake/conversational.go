package fake

import (
	"context"
	"sync"
	"time"

	"github.com/bobbyrc/onett/internal/harness"
)

// Conversational is a fake harness that behaves like claude-code with a long-lived process: turns
// that name the same Spec.ConversationID share one fake "process" until Spec.KeepAlive has passed
// since the last turn ended, or a turn is cancelled. It runs the script once per turn, as Start
// does, and records which process answered each turn.
type Conversational struct {
	inner *runner

	mu      sync.Mutex
	procs   map[string]*proc
	started int
	turns   []Turn
}

// Turn is one turn the Conversational harness ran.
type Turn struct {
	ConversationID string
	// Process numbers the fake process that answered, from 1 in start order. Turns without a
	// conversation get process 0.
	Process int
	// Resume is Spec.ResumeSessionID: what a real process would be started with.
	Resume string
	Prompt string
}

type proc struct {
	id    int
	timer *time.Timer
	// turns counts the turns started on the process, so a turn that ended knows whether a newer
	// one has begun since.
	turns int
}

var _ harness.Harness = (*Conversational)(nil)

// NewConversational returns a harness whose sessions run s.
func NewConversational(s Script) *Conversational {
	return &Conversational{inner: &runner{script: s}, procs: map[string]*proc{}}
}

// Start runs one turn.
func (c *Conversational) Start(ctx context.Context, spec harness.Spec) (harness.Session, error) {
	if spec.ConversationID == "" || spec.KeepAlive <= 0 {
		c.mu.Lock()
		c.turns = append(c.turns, Turn{Resume: spec.ResumeSessionID, Prompt: spec.Prompt})
		c.mu.Unlock()
		return c.inner.Start(ctx, spec)
	}
	c.mu.Lock()
	p := c.procs[spec.ConversationID]
	if p == nil {
		c.started++
		p = &proc{id: c.started}
		c.procs[spec.ConversationID] = p
	}
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.turns++
	turn := p.turns
	c.turns = append(c.turns, Turn{
		ConversationID: spec.ConversationID, Process: p.id, Resume: spec.ResumeSessionID, Prompt: spec.Prompt,
	})
	c.mu.Unlock()

	// Like a real long-lived process, a turn whose context ends midway kills the process. That is
	// decided when the script returns, and the process is dropped before the session is done, so
	// the next turn cannot land on it. Asking the context later would be wrong: the engine cancels
	// a turn's context right after its result, and that is not a cancelled turn.
	one := &runner{script: func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		res, err := c.inner.script(ctx, spec, emit)
		if ctx.Err() != nil {
			c.drop(spec.ConversationID, p)
		}
		return res, err
	}}
	sess, err := one.Start(ctx, spec)
	if err != nil {
		return nil, err
	}
	s := sess.(*session)
	go func() {
		<-s.done
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.procs[spec.ConversationID] != p || p.turns != turn {
			return // the process is gone, or already serves a newer turn
		}
		p.timer = time.AfterFunc(spec.KeepAlive, func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			// Not when a newer turn has started: the timer fired just as the process was reused.
			if c.procs[spec.ConversationID] == p && p.turns == turn {
				delete(c.procs, spec.ConversationID)
			}
		})
	}()
	return &convSession{session: s, drop: func() { c.drop(spec.ConversationID, p) }}, nil
}

func (c *Conversational) drop(conv string, p *proc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.procs[conv] == p {
		delete(c.procs, conv)
	}
}

// Turns returns every turn run so far, oldest first.
func (c *Conversational) Turns() []Turn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Turn(nil), c.turns...)
}

// Processes is how many fake processes have been started.
func (c *Conversational) Processes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.started
}

// Live is how many fake processes are alive now.
func (c *Conversational) Live() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.procs)
}

// convSession is a turn's session; cancelling it during the turn ends the process too.
type convSession struct {
	*session
	drop func()
}

// Cancel kills the process when the turn is still running. A Cancel that arrives after the turn
// ended (the engine cancels a turn's context right after its result) leaves the process alone,
// as the real harness does: it is idle, or already serves the next turn.
func (s *convSession) Cancel() {
	select {
	case <-s.done:
	default:
		s.drop()
	}
	s.session.Cancel()
}
