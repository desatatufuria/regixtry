package compose

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errFakePgNotReady = errors.New("fake: postgres is not accepting connections")

func testProject(bundled bool) Project {
	return Project{
		Name:            "regixtry",
		Dir:             "/tmp/regixtry-project",
		ComposeFilePath: "/tmp/regixtry-project/docker-compose.yml",
		EnvFilePath:     "/tmp/regixtry-project/regixtry.env",
		Image:           "ghcr.io/desatatufuria/regixtry:latest",
		ServiceNames:    []string{"postgres", "regixtry"},
		VolumeNames:     []string{"postgres-data", "registry-data"},
		BundledPostgres: bundled,
		PublicURL:       "http://127.0.0.1:5000",
	}
}

// TestStartDatabaseNoOpWhenExternal is the RED test for tasks.md 4.1 (first
// half): an external DSN project must never trigger `docker compose up`
// against the bundled postgres service.
func TestStartDatabaseNoOpWhenExternal(t *testing.T) {
	t.Parallel()

	fake := newFakeExec()
	p := NewProvisioner(ProvisionerConfig{Exec: fake.Run})

	if err := p.StartDatabase(context.Background(), testProject(false)); err != nil {
		t.Fatalf("StartDatabase() error = %v, want nil for an external project", err)
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("calls = %#v, want zero docker invocations for an external project", fake.Calls)
	}
}

// TestStartDatabaseBundledRunsUpAndPolls is the RED test for tasks.md 4.1
// (second half): a bundled project runs `docker compose up -d postgres`
// then polls readiness via `docker compose exec -T postgres pg_isready`.
func TestStartDatabaseBundledRunsUpAndPolls(t *testing.T) {
	t.Parallel()

	proj := testProject(true)
	fake := newFakeExec()
	fake.On("docker "+joinArgs(composeArgs(proj, "up", "-d", "postgres")), func([]execCall) ([]byte, error) {
		return []byte("ok"), nil
	})
	fake.On("docker "+joinArgs(composeArgs(proj, "exec", "-T", "postgres", "pg_isready", "-U", "registry", "-d", "regixtry_auth")), func([]execCall) ([]byte, error) {
		return []byte("accepting connections"), nil
	})
	p := NewProvisioner(ProvisionerConfig{Exec: fake.Run, Sleep: func(time.Duration) {}})

	if err := p.StartDatabase(context.Background(), proj); err != nil {
		t.Fatalf("StartDatabase() error = %v, want nil", err)
	}
	if len(fake.Calls) != 2 {
		t.Fatalf("calls = %#v, want exactly one up call and one pg_isready call", fake.Calls)
	}
	if fake.Calls[0].Args[len(fake.Calls[0].Args)-3] != "up" {
		t.Fatalf("first call = %#v, want `up` to run before the readiness poll", fake.Calls[0])
	}
}

// TestStartDatabasePollIsBounded is the RED test for tasks.md 4.2: pg_isready
// failing forever must NOT loop unboundedly -- StartDatabase must give up
// after a fixed number of attempts and return an error, not hang.
func TestStartDatabasePollIsBounded(t *testing.T) {
	t.Parallel()

	proj := testProject(true)
	fake := newFakeExec()
	fake.On("docker "+joinArgs(composeArgs(proj, "up", "-d", "postgres")), func([]execCall) ([]byte, error) {
		return []byte("ok"), nil
	})
	fake.Default = func(execCall) ([]byte, error) {
		return nil, errFakePgNotReady
	}

	var sleeps int
	p := NewProvisioner(ProvisionerConfig{Exec: fake.Run, Sleep: func(time.Duration) { sleeps++ }})

	err := p.StartDatabase(context.Background(), proj)
	if err == nil {
		t.Fatalf("StartDatabase() error = nil, want a bounded-timeout error when pg_isready never succeeds")
	}

	pollCalls := 0
	for _, call := range fake.Calls {
		for _, arg := range call.Args {
			if arg == "pg_isready" {
				pollCalls++
				break
			}
		}
	}
	if pollCalls == 0 || pollCalls > 60 {
		t.Fatalf("poll calls = %d (total calls %#v), want a small bounded retry count, not zero and not unbounded", pollCalls, fake.Calls)
	}
	if sleeps == 0 {
		t.Fatalf("sleeps = 0, want at least one sleep between poll attempts")
	}
}
