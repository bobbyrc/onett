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
	c.turns = append(c.turns, Turn{
		ConversationID: spec.ConversationID, Process: p.id, Resume: spec.ResumeSessionID, Prompt: spec.Prompt,
	})
	c.mu.Unlock()

	// Like a real long-lived process, the script outlives the turn's context only until the turn
	// ends; cancelling a turn kills the process.
	sess, err := c.inner.Start(ctx, spec)
	if err != nil {
		return nil, err
	}
	s := sess.(*session)
	go func() {
		<-s.done
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.procs[spec.ConversationID] == p && s.ctx.Err() == nil {
			p.timer = time.AfterFunc(spec.KeepAlive, func() { c.drop(spec.ConversationID, p) })
		}
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

// convSession is a turn's session; cancelling it ends the process too.
type convSession struct {
	*session
	drop func()
}

func (s *convSession) Cancel() {
	s.drop()
	s.session.Cancel()
}
