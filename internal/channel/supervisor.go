package channel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/bobbyrc/onett/internal/config"
)

// Supervisor runs the configured chat bots and restarts the ones whose settings change. Only
// the first channel of each type is used.
type Supervisor struct {
	host   Host
	logger *slog.Logger
	// ctx is the daemon's lifetime; every bot's context derives from it.
	ctx context.Context

	mu      sync.Mutex
	running map[string]*runningBot // keyed by channel type
	wg      sync.WaitGroup
}

type runningBot struct {
	fingerprint  string
	skipNotifier string
	bot          Bot
	cancel       context.CancelFunc
	done         chan struct{}
}

// NewSupervisor returns a Supervisor whose bots stop when ctx ends. Wait blocks until they have.
func NewSupervisor(ctx context.Context, host Host, logger *slog.Logger) *Supervisor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Supervisor{ctx: ctx, host: host, logger: logger, running: map[string]*runningBot{}}
}

// Wait blocks until every bot has exited. Cancel the context given to NewSupervisor first.
func (s *Supervisor) Wait() { s.wg.Wait() }

type want struct {
	typ          string
	fingerprint  string
	skipNotifier string
	start        Starter
}

// resolve validates every channel and resolves its token, keeping only the first channel of
// each type.
func resolve(chans []config.Channel, env Env) ([]want, error) {
	var wants []want
	seen := map[string]bool{}
	for _, ch := range chans {
		if seen[ch.Type] {
			continue
		}
		seen[ch.Type] = true
		reg, ok := Lookup(ch.Type)
		if !ok {
			return nil, fmt.Errorf("channel %q: unknown type (known: %s)", ch.Type, strings.Join(Types(), ", "))
		}
		// Record which environment variables the factory reads so the fingerprint covers their
		// values (a changed token) without the values ever being printed or stored.
		rec := &envRecorder{getenv: env.Getenv}
		e := env
		e.Getenv = rec.get
		start, err := reg.Factory(ch, e)
		if err != nil {
			return nil, err
		}
		wants = append(wants, want{typ: ch.Type, fingerprint: fingerprint(ch, rec), skipNotifier: reg.SkipApprovalsForNotifier, start: start})
	}
	return wants, nil
}

// Resolve checks the channels without starting anything, so a bad config or an empty token
// variable can stop startup. It returns the notifier types that must not send approval requests
// while a channel runs.
func Resolve(chans []config.Channel, env Env) (map[string]bool, error) {
	wants, err := resolve(chans, env)
	if err != nil {
		return nil, err
	}
	skip := map[string]bool{}
	for _, w := range wants {
		if w.skipNotifier != "" {
			skip[w.skipNotifier] = true
		}
	}
	return skip, nil
}

// SkipApprovals returns the notifier types that must not send approval requests because a
// channel that carries them is running.
func (s *Supervisor) SkipApprovals() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	skip := map[string]bool{}
	for _, b := range s.running {
		if b.skipNotifier != "" {
			skip[b.skipNotifier] = true
		}
	}
	return skip
}

// Reconcile makes the running bots match chans. A channel whose type, options and referenced
// environment values are unchanged keeps running; a changed one is stopped (and waited for)
// before its replacement starts; a removed one is stopped. Every channel is validated and its
// token resolved before anything is stopped, so a bad config leaves the old bots running and
// returns the error. A bot that fails to start is logged and skipped, never fatal.
func (s *Supervisor) Reconcile(chans []config.Channel, env Env) error {
	wants, err := resolve(chans, env)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	keep := map[string]bool{}
	for _, w := range wants {
		keep[w.typ] = true
	}
	for typ, b := range s.running {
		if !keep[typ] {
			s.stop(typ, b)
		}
	}
	for _, w := range wants {
		if b, ok := s.running[w.typ]; ok {
			if b.fingerprint == w.fingerprint {
				continue
			}
			s.stop(w.typ, b)
		}
		if w.start == nil {
			continue
		}
		bot, err := w.start(s.host)
		if err != nil {
			s.logger.Error(w.typ+" channel not started", "err", err)
			continue
		}
		bctx, cancel := context.WithCancel(s.ctx)
		b := &runningBot{fingerprint: w.fingerprint, skipNotifier: w.skipNotifier, bot: bot, cancel: cancel, done: make(chan struct{})}
		s.running[w.typ] = b
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer close(b.done)
			if err := bot.Run(bctx); err != nil && !errors.Is(err, context.Canceled) {
				s.logger.Error(w.typ+" channel stopped", "err", err)
			}
		}()
	}
	return nil
}

// Test sends a test message through the running bot of ch's type. ErrNotRunning (wrapped, with
// the reason: an unset token variable, a bad option, or another channel of the same type being
// the one that runs) when no bot runs for it, ErrNoTest when the bot cannot send one.
func (s *Supervisor) Test(ctx context.Context, ch config.Channel, env Env) error {
	s.mu.Lock()
	b := s.running[ch.Type]
	s.mu.Unlock()
	if b == nil {
		if _, err := resolve([]config.Channel{ch}, env); err != nil {
			return fmt.Errorf("%w: %v", ErrNotRunning, err)
		}
		return fmt.Errorf("%w: its bot has not started; see the daemon log", ErrNotRunning)
	}
	t, ok := b.bot.(Tester)
	if !ok {
		return fmt.Errorf("%w (%s)", ErrNoTest, ch.Type)
	}
	return t.SendTest(ctx)
}

// stop cancels a bot and waits for it to exit. The caller holds s.mu.
func (s *Supervisor) stop(typ string, b *runningBot) {
	b.cancel()
	<-b.done
	delete(s.running, typ)
}

// envRecorder remembers the environment variables a factory looked up.
type envRecorder struct {
	getenv func(string) string
	names  map[string]string
}

func (r *envRecorder) get(name string) string {
	v := r.getenv(name)
	if r.names == nil {
		r.names = map[string]string{}
	}
	r.names[name] = v
	return v
}

// fingerprint hashes a channel's type, options and the environment values it read. Only the
// digest is kept.
func fingerprint(ch config.Channel, rec *envRecorder) string {
	h := sha256.New()
	opts, _ := json.Marshal(ch.Options) // map keys marshal sorted
	fmt.Fprintf(h, "%s\x00%s\x00", ch.Type, opts)
	names := make([]string, 0, len(rec.names))
	for n := range rec.names {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(h, "%s=%s\x00", n, rec.names[n])
	}
	return hex.EncodeToString(h.Sum(nil))
}
