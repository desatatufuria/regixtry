package main

import (
	"strings"
	"testing"
)

func TestFormatSetupCommandNeverEchoesTheDSNPassword(t *testing.T) {
	const password = "hunter2-secret"
	cfg := BootstrapConfig{
		Addr:            "127.0.0.1:5000",
		StorageRoot:     "/var/lib/regixtry",
		StatePath:       "/etc/regixtry/bootstrap-state.json",
		UnitPath:        "/etc/systemd/system/regixtry.service",
		ServiceName:     "regixtry",
		AuthPostgresDSN: "postgres://registry:" + password + "@db.example.com:5432/regixtry_auth?sslmode=disable",
	}

	for _, euid := range []int{1000, 0} {
		restore := swapCurrentEUID(t, euid)
		hint := formatSetupCommand(cfg, false)
		restore()

		if strings.Contains(hint, password) {
			t.Fatalf("euid %d: hint leaks the password:\n%s", euid, hint)
		}
		if strings.Contains(hint, "--auth-postgres-dsn") {
			t.Fatalf("euid %d: hint passes the DSN as an argument:\n%s", euid, hint)
		}
		if !strings.Contains(hint, "REGISTRY_AUTH_POSTGRES_DSN") {
			t.Fatalf("euid %d: hint does not tell the operator to use the environment variable:\n%s", euid, hint)
		}
		if !strings.Contains(hint, "postgres://registry:xxxxx@db.example.com") {
			t.Fatalf("euid %d: hint should show the redacted DSN:\n%s", euid, hint)
		}
		if euid != 0 && !strings.Contains(hint, "sudo --preserve-env=REGISTRY_AUTH_POSTGRES_DSN ") {
			t.Fatalf("euid %d: sudo must preserve the variable:\n%s", euid, hint)
		}
	}
}

func TestFormatSetupCommandWithoutDSNIsUnchanged(t *testing.T) {
	restore := swapCurrentEUID(t, 1000)
	defer restore()
	hint := formatSetupCommand(BootstrapConfig{Addr: "a", StorageRoot: "s", StatePath: "p", UnitPath: "u", ServiceName: "regixtry"}, false)
	if strings.Contains(hint, "REGISTRY_AUTH_POSTGRES_DSN") || !strings.HasPrefix(hint, "sudo ") {
		t.Fatalf("hint = %q, want a plain sudo command without the DSN guidance", hint)
	}
}
