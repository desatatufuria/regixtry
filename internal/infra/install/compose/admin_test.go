package compose

import (
	"context"
	"errors"
	"strings"
	"testing"
)

var errBootstrapAdminFailedFake = errors.New("fake: bootstrap-admin exited non-zero")

// TestBootstrapAdminPipesPasswordViaStdinOnly is the RED test for tasks.md
// 4.4: the admin password must reach `bootstrap-admin -password-stdin`
// exclusively via stdin -- never as an argv element and never as an
// environment variable (design.md "Secret channel" threat).
func TestBootstrapAdminPipesPasswordViaStdinOnly(t *testing.T) {
	t.Parallel()

	proj := testProject(true)
	fake := newFakeExec()
	wantArgs := composeArgs(proj, "run", "--rm", "--no-deps", "-T", "regixtry", "bootstrap-admin", "-username", "admin", "-password-stdin")
	fake.On("docker "+joinArgs(wantArgs), func([]execCall) ([]byte, error) { return []byte("ok"), nil })
	p := NewProvisioner(ProvisionerConfig{ExecStdin: fake.RunStdin})

	const password = "s3cr3t-admin-pw"
	if err := p.BootstrapAdmin(context.Background(), proj, "admin", password); err != nil {
		t.Fatalf("BootstrapAdmin() error = %v, want nil", err)
	}

	if len(fake.Calls) != 1 {
		t.Fatalf("calls = %#v, want exactly one docker compose run invocation", fake.Calls)
	}
	call := fake.Calls[0]

	for _, arg := range call.Args {
		if strings.Contains(arg, password) {
			t.Fatalf("argv element %q contains the admin password; it must only ever be piped via stdin", arg)
		}
	}
	if call.Stdin != password {
		t.Fatalf("call.Stdin = %q, want exactly the admin password %q", call.Stdin, password)
	}
	if got := joinArgs(call.Args); got != joinArgs(wantArgs) {
		t.Fatalf("args = %q, want %q", got, joinArgs(wantArgs))
	}
}

// TestBootstrapAdminArgvSafety is the RED test for tasks.md 4.5: an
// operator-supplied project name or username containing `;`, `$(...)`,
// spaces, or a leading `-` must reach "docker" as one literal argv
// element, never shell-interpreted (design.md "Subprocess argv
// composition" threat).
func TestBootstrapAdminArgvSafety(t *testing.T) {
	t.Parallel()

	proj := testProject(true)
	proj.Name = "regixtry; rm -rf / $(whoami) -x"
	weirdUsername := "admin; rm -rf / $(whoami) -x"

	fake := newFakeExec()
	fake.Default = func(execCall) ([]byte, error) { return []byte("ok"), nil }
	p := NewProvisioner(ProvisionerConfig{ExecStdin: fake.RunStdin})

	if err := p.BootstrapAdmin(context.Background(), proj, weirdUsername, "pw"); err != nil {
		t.Fatalf("BootstrapAdmin() error = %v, want nil", err)
	}

	if len(fake.Calls) != 1 {
		t.Fatalf("calls = %#v, want exactly one invocation", fake.Calls)
	}
	call := fake.Calls[0]

	foundProjectName := false
	foundUsername := false
	for _, arg := range call.Args {
		if arg == proj.Name {
			foundProjectName = true
		}
		if arg == weirdUsername {
			foundUsername = true
		}
	}
	if !foundProjectName {
		t.Fatalf("args = %#v, want the project name to reach docker as one literal argv element", call.Args)
	}
	if !foundUsername {
		t.Fatalf("args = %#v, want the username to reach docker as one literal argv element", call.Args)
	}
	if call.Name != "docker" {
		t.Fatalf("call.Name = %q, want exactly \"docker\" (never a shell)", call.Name)
	}
}

// TestBootstrapAdminReturnsWrappedErrorWithoutLeakingPassword triangulates
// 4.4/4.6: when the underlying docker invocation fails, BootstrapAdmin must
// still surface a truthful, wrapped error, and that error text must never
// contain the admin password.
func TestBootstrapAdminReturnsWrappedErrorWithoutLeakingPassword(t *testing.T) {
	t.Parallel()

	proj := testProject(true)
	fake := newFakeExec()
	fake.Default = func(execCall) ([]byte, error) { return nil, errBootstrapAdminFailedFake }
	p := NewProvisioner(ProvisionerConfig{ExecStdin: fake.RunStdin})

	const password = "another-secret-pw"
	err := p.BootstrapAdmin(context.Background(), proj, "admin", password)
	if err == nil {
		t.Fatalf("BootstrapAdmin() error = nil, want the underlying failure to be surfaced")
	}
	if !strings.Contains(err.Error(), errBootstrapAdminFailedFake.Error()) {
		t.Fatalf("error = %v, want it to wrap the underlying failure", err)
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("error = %v, want the admin password to never appear in error output", err)
	}
}
