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
// then polls readiness via `docker compose logs postgres`.
func TestStartDatabaseBundledRunsUpAndPolls(t *testing.T) {
	t.Parallel()

	proj := testProject(true)
	fake := newFakeExec()
	fake.On("docker "+joinArgs(composeArgs(proj, "up", "-d", "postgres")), func([]execCall) ([]byte, error) {
		return []byte("ok"), nil
	})
	fake.On("docker "+joinArgs(composeArgs(proj, "logs", "--no-color", "postgres")), func([]execCall) ([]byte, error) {
		return []byte("database system is ready to accept connections"), nil
	})
	p := NewProvisioner(ProvisionerConfig{Exec: fake.Run, Sleep: func(time.Duration) {}})

	if err := p.StartDatabase(context.Background(), proj); err != nil {
		t.Fatalf("StartDatabase() error = %v, want nil", err)
	}
	if len(fake.Calls) != 2 {
		t.Fatalf("calls = %#v, want exactly one up call and one readiness-log call", fake.Calls)
	}
	if fake.Calls[0].Args[len(fake.Calls[0].Args)-3] != "up" {
		t.Fatalf("first call = %#v, want `up` to run before the readiness poll", fake.Calls[0])
	}
}

// TestStartDatabaseFreshVolumeWaitsForSecondRestart is the RED test proving
// the fix for the real bug found during v0.2.1-rc4's manual docker-mode
// validation: on a FRESH volume, the official postgres image starts a
// temporary Unix-socket-only server to run initdb, shuts it down, then
// starts the final server that actually accepts networked, password-
// authenticated connections. `pg_isready` run inside the postgres container
// (the previous implementation) can report success against that temporary
// server -- confirmed live: BootstrapAdmin's very next step, connecting over
// the network with the real password, then failed deterministically on
// every fresh volume, and only succeeded once Postgres had already been
// running long enough to complete both restarts. The postgres image logs
// "PostgreSQL init process complete; ready for start up." exactly when that
// handoff happens, and "database system is ready to accept connections"
// once per server start -- twice on a fresh volume. A single occurrence
// alongside the reinit marker must NOT be treated as ready.
func TestStartDatabaseFreshVolumeWaitsForSecondRestart(t *testing.T) {
	t.Parallel()

	proj := testProject(true)
	fake := newFakeExec()
	fake.On("docker "+joinArgs(composeArgs(proj, "up", "-d", "postgres")), func([]execCall) ([]byte, error) {
		return []byte("ok"), nil
	})

	logCalls := 0
	logKey := "docker " + joinArgs(composeArgs(proj, "logs", "--no-color", "postgres"))
	fake.On(logKey, func([]execCall) ([]byte, error) {
		logCalls++
		if logCalls == 1 {
			// Only the temporary initdb server has started and become
			// "ready" so far -- the log line proving a restart is coming,
			// but the final server hasn't logged its own readiness yet.
			return []byte(
				"database system is ready to accept connections\n" +
					"PostgreSQL init process complete; ready for start up.\n",
			), nil
		}
		// The final server has now also logged readiness -- the marker
		// appears a second time.
		return []byte(
			"database system is ready to accept connections\n" +
				"PostgreSQL init process complete; ready for start up.\n" +
				"database system is ready to accept connections\n",
		), nil
	})
	p := NewProvisioner(ProvisionerConfig{Exec: fake.Run, Sleep: func(time.Duration) {}})

	if err := p.StartDatabase(context.Background(), proj); err != nil {
		t.Fatalf("StartDatabase() error = %v, want nil once the final server's readiness is logged", err)
	}
	if logCalls < 2 {
		t.Fatalf("log polls = %d, want StartDatabase to poll again instead of trusting the temporary server's single readiness line", logCalls)
	}
}

// TestStartDatabasePollIsBounded is the RED test for tasks.md 4.2: readiness
// logs never appearing must NOT loop unboundedly -- StartDatabase must give
// up after a fixed number of attempts and return an error, not hang.
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
		t.Fatalf("StartDatabase() error = nil, want a bounded-timeout error when readiness logs never appear")
	}

	pollCalls := 0
	for _, call := range fake.Calls {
		for _, arg := range call.Args {
			if arg == "logs" {
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
