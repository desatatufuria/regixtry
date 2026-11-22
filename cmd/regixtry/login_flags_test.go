package main

import (
	"strings"
	"testing"
)

func TestParseServeConfigTrustedProxiesDefaultsToNone(t *testing.T) {
	t.Setenv("REGISTRY_TRUSTED_PROXIES", "")
	cfg, err := parseServeConfig(nil)
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}
	if cfg.TrustedProxies != "" {
		t.Fatalf("TrustedProxies = %q, want empty (X-Forwarded-For ignored)", cfg.TrustedProxies)
	}
}

func TestParseServeConfigTrustedProxiesFlagAndEnv(t *testing.T) {
	t.Setenv("REGISTRY_TRUSTED_PROXIES", "10.0.0.0/8")
	cfg, err := parseServeConfig(nil)
	if err != nil || cfg.TrustedProxies != "10.0.0.0/8" {
		t.Fatalf("env: cfg=%q err=%v", cfg.TrustedProxies, err)
	}
	cfg, err = parseServeConfig([]string{"-trusted-proxies", "192.168.1.245,fd00::/8"})
	if err != nil || cfg.TrustedProxies != "192.168.1.245,fd00::/8" {
		t.Fatalf("flag must override env: cfg=%q err=%v", cfg.TrustedProxies, err)
	}
}

func TestParseServeConfigRejectsInvalidTrustedProxies(t *testing.T) {
	t.Setenv("REGISTRY_TRUSTED_PROXIES", "")
	_, err := parseServeConfig([]string{"-trusted-proxies", "10.0.0.0/99"})
	if err == nil || !strings.Contains(err.Error(), "trusted proxy") {
		t.Fatalf("err = %v, want a trusted proxy error", err)
	}
}

func TestParseServeConfigLoginMaxConcurrency(t *testing.T) {
	t.Setenv("REGISTRY_LOGIN_MAX_CONCURRENCY", "")
	cfg, err := parseServeConfig(nil)
	if err != nil || cfg.LoginMaxConcurrency != 0 {
		t.Fatalf("default = %d, %v; want 0 (automatic)", cfg.LoginMaxConcurrency, err)
	}
	t.Setenv("REGISTRY_LOGIN_MAX_CONCURRENCY", "6")
	if cfg, err = parseServeConfig(nil); err != nil || cfg.LoginMaxConcurrency != 6 {
		t.Fatalf("env = %d, %v", cfg.LoginMaxConcurrency, err)
	}
	if cfg, err = parseServeConfig([]string{"-login-max-concurrency", "3"}); err != nil || cfg.LoginMaxConcurrency != 3 {
		t.Fatalf("flag = %d, %v", cfg.LoginMaxConcurrency, err)
	}
	t.Setenv("REGISTRY_LOGIN_MAX_CONCURRENCY", "lots")
	if _, err = parseServeConfig(nil); err == nil {
		t.Fatal("a non-numeric env value must be a startup error")
	}
}
