package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/scm"
	"github.com/bobbyrc/onett/internal/store"
)

// This file holds the fault injection shared by the critical_*_test.go files. Those tests check
// what the approval, merge, startup-recovery and shipped-task paths do when a store or SCM call
// fails part-way through, or when the owner moves a task while a step is in flight.

// errCPInjected is the failure cpStore returns for a call a test chose to fail.
var errCPInjected = errors.New("injected store failure")

// cpStore wraps the engine's store so a test can fail a chosen call, or act right after one (as
// an owner cancelling a task mid-step would). Calls are named as on store.Store ("UpdateTask")
// and counted from 1, from the moment the wrapper is installed.
type cpStore struct {
	store.Store
	mu    sync.Mutex
	calls map[string]int
	fail  map[string]func(n int) bool
	after map[string]func()
}

// cpWrap puts a cpStore between e and its store. Call it after seeding and before the engine
// acts; tests keep reading and seeding through the raw store.
func cpWrap(e *Engine) *cpStore {
	s := &cpStore{
		Store: e.d.Store,
		calls: map[string]int{},
		fail:  map[string]func(int) bool{},
		after: map[string]func(){},
	}
	e.d.Store = s
	return s
}

// failOn makes method fail with errCPInjected whenever when reports true for its call number.
// when runs on the calling goroutine; it may use the raw store, never this wrapper.
func (s *cpStore) failOn(method string, when func(n int) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail[method] = when
}

// afterCall runs fn after each call of method that succeeded, before the engine sees the result.
func (s *cpStore) afterCall(method string, fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.after[method] = fn
}

// heal removes every injected failure and hook, so the store works again.
func (s *cpStore) heal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.fail)
	clear(s.after)
}

// cpAlways fails every call.
func cpAlways(int) bool { return true }

// cpCall fails only call number k.
func cpCall(k int) func(int) bool {
	return func(n int) bool { return n == k }
}

// cpDo counts one call of method, fails it if the test asked for that, and otherwise runs it and
// then the method's after hook.
func cpDo[T any](s *cpStore, method string, call func() (T, error)) (T, error) {
	s.mu.Lock()
	s.calls[method]++
	n, when, after := s.calls[method], s.fail[method], s.after[method]
	s.mu.Unlock()
	if when != nil && when(n) {
		var zero T
		return zero, fmt.Errorf("%s: %w", method, errCPInjected)
	}
	out, err := call()
	if err == nil && after != nil {
		after()
	}
	return out, err
}

func (s *cpStore) GetTask(ctx context.Context, id string) (domain.Task, error) {
	return cpDo(s, "GetTask", func() (domain.Task, error) { return s.Store.GetTask(ctx, id) })
}

func (s *cpStore) ListTasks(ctx context.Context, f store.TaskFilter) ([]domain.Task, error) {
	return cpDo(s, "ListTasks", func() ([]domain.Task, error) { return s.Store.ListTasks(ctx, f) })
}

func (s *cpStore) UpdateTask(ctx context.Context, id string, fn func(*domain.Task) error) (domain.Task, error) {
	return cpDo(s, "UpdateTask", func() (domain.Task, error) { return s.Store.UpdateTask(ctx, id, fn) })
}

func (s *cpStore) ListRuns(ctx context.Context, f store.RunFilter) ([]domain.Run, error) {
	return cpDo(s, "ListRuns", func() ([]domain.Run, error) { return s.Store.ListRuns(ctx, f) })
}

func (s *cpStore) UpdateRun(ctx context.Context, id string, fn func(*domain.Run) error) (domain.Run, error) {
	return cpDo(s, "UpdateRun", func() (domain.Run, error) { return s.Store.UpdateRun(ctx, id, fn) })
}

func (s *cpStore) ListApprovals(ctx context.Context, status domain.ApprovalStatus) ([]domain.Approval, error) {
	return cpDo(s, "ListApprovals", func() ([]domain.Approval, error) { return s.Store.ListApprovals(ctx, status) })
}

func (s *cpStore) CreateApproval(ctx context.Context, a domain.Approval) (domain.Approval, error) {
	return cpDo(s, "CreateApproval", func() (domain.Approval, error) { return s.Store.CreateApproval(ctx, a) })
}

func (s *cpStore) GetLoopState(ctx context.Context, name, project string) (domain.LoopState, error) {
	return cpDo(s, "GetLoopState", func() (domain.LoopState, error) { return s.Store.GetLoopState(ctx, name, project) })
}

func (s *cpStore) ListLoopStates(ctx context.Context) ([]domain.LoopState, error) {
	return cpDo(s, "ListLoopStates", func() ([]domain.LoopState, error) { return s.Store.ListLoopStates(ctx) })
}

func (s *cpStore) ListConversations(ctx context.Context, limit int) ([]domain.Conversation, error) {
	return cpDo(s, "ListConversations", func() ([]domain.Conversation, error) { return s.Store.ListConversations(ctx, limit) })
}

func (s *cpStore) UpdateConversation(ctx context.Context, id string, fn func(*domain.Conversation) error) (domain.Conversation, error) {
	return cpDo(s, "UpdateConversation", func() (domain.Conversation, error) { return s.Store.UpdateConversation(ctx, id, fn) })
}

// cpSCM wraps the fake SCM so a test can see which head commit each merge was pinned to, act
// right after a merge, or fail a PR read.
type cpSCM struct {
	scm.SCM
	// afterMerge, when set, runs after a MergePR that succeeded.
	afterMerge func()
	// getPRErr, when set, is asked before each GetPR; a non-nil error fails that read.
	getPRErr func() error

	mu         sync.Mutex
	mergeHeads []string
}

// cpWrapSCM makes e use a cpSCM around inner for every project. Set the hooks before the engine
// acts.
func cpWrapSCM(e *Engine, inner scm.SCM) *cpSCM {
	s := &cpSCM{SCM: inner}
	e.d.SCMFor = func(*config.Project) (scm.SCM, error) { return s, nil }
	return s
}

func (s *cpSCM) MergePR(ctx context.Context, number int, headSHA string) error {
	s.mu.Lock()
	s.mergeHeads = append(s.mergeHeads, headSHA)
	s.mu.Unlock()
	err := s.SCM.MergePR(ctx, number, headSHA)
	if err == nil && s.afterMerge != nil {
		s.afterMerge()
	}
	return err
}

func (s *cpSCM) GetPR(ctx context.Context, number int) (scm.PR, error) {
	if s.getPRErr != nil {
		if err := s.getPRErr(); err != nil {
			return scm.PR{}, err
		}
	}
	return s.SCM.GetPR(ctx, number)
}

// heads returns the head commit each MergePR call asked for, in order.
func (s *cpSCM) heads() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.mergeHeads)
}
