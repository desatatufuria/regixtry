package compose

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// execCall records one invocation of the exec seam for assertions. Shared
// across every compose package test file (PR #1's Preflight/WriteProject
// suites through PR #2/#3's StartDatabase/BootstrapAdmin/StartRegistry/...
// suites) -- tasks.md 1.3 REFACTOR note. Stdin is populated only by RunStdin
// calls (BootstrapAdmin's -password-stdin path); it stays empty for every
// argv-only call, so a test can assert a secret appears in exactly one
// call's Stdin field and in zero calls' Args.
type execCall struct {
	Name  string
	Args  []string
	Stdin string
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
	return f.dispatch(call)
}

// RunStdin implements execStdinRunner; pass it as ProvisionerConfig.ExecStdin
// in tests. The stdin reader is fully consumed and recorded on the call so a
// test can assert exactly what a secret-bearing call received on stdin --
// never on argv (BootstrapAdmin's -password-stdin path, design.md "Secret
// channel" threat).
func (f *fakeExec) RunStdin(_ context.Context, stdin io.Reader, name string, args ...string) ([]byte, error) {
	var stdinBody string
	if stdin != nil {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("fakeExec: read stdin: %w", err)
		}
		stdinBody = string(data)
	}
	call := execCall{Name: name, Args: append([]string{}, args...), Stdin: stdinBody}
	f.Calls = append(f.Calls, call)
	return f.dispatch(call)
}

// joinArgs renders an argv slice as the space-joined key fakeExec.On/Run use
// internally, so tests can register/assert handlers against composeArgs(...)
// output without duplicating the join logic.
func joinArgs(args []string) string {
	return strings.Join(args, " ")
}

func (f *fakeExec) dispatch(call execCall) ([]byte, error) {
	key := strings.TrimSpace(strings.Join(append([]string{call.Name}, call.Args...), " "))
	if handler, ok := f.handlers[key]; ok {
		return handler(append([]execCall{}, f.Calls...))
	}
	if f.Default != nil {
		return f.Default(call)
	}
	return nil, fmt.Errorf("fakeExec: no handler registered for %q", key)
}
