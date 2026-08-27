package compose

import (
	"context"
	"fmt"
	"strings"
)

// execCall records one invocation of the exec seam for assertions. Shared
// across every compose package test file (PR #1's Preflight/WriteProject
// suites through PR #2/#3's StartDatabase/BootstrapAdmin/StartRegistry/...
// suites) -- tasks.md 1.3 REFACTOR note.
type execCall struct {
	Name string
	Args []string
}

// fakeExec is a minimal, reusable fake for the compose package's exec seam.
// Register a canned handler per exact "name arg1 arg2 ..." command; every
// call is recorded regardless of whether a handler matched, so tests can
// assert on ordering/argv shape even for calls with no registered handler.
type fakeExec struct {
	Calls    []execCall
	handlers map[string]func([]execCall) ([]byte, error)
	Default  func(execCall) ([]byte, error)
}

func newFakeExec() *fakeExec {
	return &fakeExec{handlers: make(map[string]func([]execCall) ([]byte, error))}
}

// On registers a canned handler for the exact "name arg1 arg2 ..." command.
func (f *fakeExec) On(command string, handler func([]execCall) ([]byte, error)) {
	f.handlers[command] = handler
}

// Run implements execRunner; pass it as ProvisionerConfig.Exec in tests.
func (f *fakeExec) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := execCall{Name: name, Args: append([]string{}, args...)}
	f.Calls = append(f.Calls, call)

	key := strings.TrimSpace(strings.Join(append([]string{name}, args...), " "))
	if handler, ok := f.handlers[key]; ok {
		return handler(append([]execCall{}, f.Calls...))
	}
	if f.Default != nil {
		return f.Default(call)
	}
	return nil, fmt.Errorf("fakeExec: no handler registered for %q", key)
}
