// Package observe reads production signals (errors, alerts) for a project.
package observe

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
)

// Observer reports production problems seen since a point in time.
type Observer interface {
	Poll(ctx context.Context, since time.Time) ([]domain.SignalObservation, error)
}

// Config is one entry of a project's observe list from onett.yaml.
type Config struct {
	Name    string // unique per project; becomes Signal.Source
	Type    string
	Options map[string]any
	Env     map[string]string
}

type Factory func(cfg Config) (Observer, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

func Register(typ string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[typ] = f
}

// Types lists the registered types, sorted.
func Types() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(factories))
	for t := range factories {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func New(cfg Config) (Observer, error) {
	mu.RLock()
	f, ok := factories[cfg.Type]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("observer %q: unknown type %q", cfg.Name, cfg.Type)
	}
	return f(cfg)
}
