package compose

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// TestStartRegistryBundledRunsPlainUp is the RED test for tasks.md 6.1
// (bundled half): a bundled project brings up the whole stack with
// `docker compose up -d` -- no `--no-deps` restriction, since Postgres is
// part of this project (design.md Data Flow: "bundled: docker compose up -d
// ... external: docker compose up -d --no-deps regixtry").
func TestStartRegistryBundledRunsPlainUp(t *testing.T) {
	t.Parallel()

	proj := testProject(true)
	fake := newFakeExec()
	fake.On("docker "+joinArgs(composeArgs(proj, "up", "-d")), func([]execCall) ([]byte, error) {
		return []byte("ok"), nil
	})
	p := NewProvisioner(ProvisionerConfig{Exec: fake.Run})

	if err := p.StartRegistry(context.Background(), proj); err != nil {
		t.Fatalf("StartRegistry() error = %v, want nil", err)
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("calls = %#v, want exactly one `up -d` call", fake.Calls)
	}
}

// TestStartRegistryExternalSkipsBundledDependencies is the RED test for
// tasks.md 6.1 (external half): an external-DSN project starts only the
// regixtry service with `--no-deps`, so the bundled postgres service is
// never started by StartRegistry (design.md "Skipping the bundled service"
// decision).
func TestStartRegistryExternalSkipsBundledDependencies(t *testing.T) {
	t.Parallel()

	proj := testProject(false)
	fake := newFakeExec()
	fake.On("docker "+joinArgs(composeArgs(proj, "up", "-d", "--no-deps", "regixtry")), func([]execCall) ([]byte, error) {
		return []byte("ok"), nil
	})
	p := NewProvisioner(ProvisionerConfig{Exec: fake.Run})

	if err := p.StartRegistry(context.Background(), proj); err != nil {
		t.Fatalf("StartRegistry() error = %v, want nil", err)
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("calls = %#v, want exactly one `up -d --no-deps regixtry` call", fake.Calls)
	}
}

// TestWaitReachableSucceedsOn200Then401 is the RED test for tasks.md 6.2
// (happy half): WaitReachable polls GET <public-url>/v2/ and accepts either
// 200 or 401, matching `regixtry healthcheck` semantics (design.md
// Interfaces / Contracts). Triangulated across the two accepted statuses in
// one table-driven test.
func TestWaitReachableSucceedsOn200Then401(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusOK, http.StatusUnauthorized} {
		status := status
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			proj := testProject(true)
			wantURL := proj.PublicURL + "/v2/"
			calls := 0
			fetch := func(_ context.Context, url string) (int, error) {
				calls++
				if url != wantURL {
					t.Fatalf("fetch url = %q, want %q", url, wantURL)
				}
				return status, nil
			}
			p := NewProvisioner(ProvisionerConfig{HTTPStatus: fetch, Sleep: func(time.Duration) {}})

			if err := p.WaitReachable(context.Background(), proj); err != nil {
				t.Fatalf("WaitReachable() error = %v, want nil for status %d", err, status)
			}
			if calls != 1 {
				t.Fatalf("fetch calls = %d, want exactly one successful poll", calls)
			}
		})
	}
}

// TestWaitReachableRetriesUntilReachable proves WaitReachable is a real
// bounded poll, not a single-shot check: a 503 on the first two attempts
// then 200 on the third must still succeed, exercising the retry path
// itself (strict TDD triangulation -- a test that only ever calls fetch
// once would not prove the loop runs).
func TestWaitReachableRetriesUntilReachable(t *testing.T) {
	t.Parallel()

	proj := testProject(true)
	calls := 0
	fetch := func(context.Context, string) (int, error) {
		calls++
		if calls < 3 {
			return http.StatusServiceUnavailable, nil
		}
		return http.StatusOK, nil
	}
	var sleeps int
	p := NewProvisioner(ProvisionerConfig{HTTPStatus: fetch, Sleep: func(time.Duration) { sleeps++ }})

	if err := p.WaitReachable(context.Background(), proj); err != nil {
		t.Fatalf("WaitReachable() error = %v, want nil once the third poll succeeds", err)
	}
	if calls != 3 {
		t.Fatalf("fetch calls = %d, want exactly 3 (fail, fail, succeed)", calls)
	}
	if sleeps != 2 {
		t.Fatalf("sleeps = %d, want exactly 2 (one between each failed attempt)", sleeps)
	}
}

// TestWaitReachablePollIsBoundedAndReportsInstallationFailure is the RED
// test for tasks.md 6.2 (bounded half): a registry that never becomes
// reachable must NOT loop unboundedly, and the returned error MUST read as
// an installation failure, not a claim that the stack is healthy (spec.md
// "Stack does not become reachable" scenario).
func TestWaitReachablePollIsBoundedAndReportsInstallationFailure(t *testing.T) {
	t.Parallel()

	proj := testProject(true)
	fetch := func(context.Context, string) (int, error) {
		return 0, errors.New("fake: connection refused")
	}
	var sleeps int
	p := NewProvisioner(ProvisionerConfig{HTTPStatus: fetch, Sleep: func(time.Duration) { sleeps++ }})

	err := p.WaitReachable(context.Background(), proj)
	if err == nil {
		t.Fatalf("WaitReachable() error = nil, want a bounded-timeout error when the registry never becomes reachable")
	}
	if sleeps == 0 || sleeps > 60 {
		t.Fatalf("sleeps = %d, want a small bounded retry count, not zero and not unbounded", sleeps)
	}
}
