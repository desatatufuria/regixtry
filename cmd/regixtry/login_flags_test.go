package main

import (
	"strings"
	"testing"
	"time"
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

func clearLoginEnv(t *testing.T) {
	for _, key := range []string{"REGISTRY_LOGIN_BACKOFF_THRESHOLD", "REGISTRY_LOGIN_BACKOFF_BASE", "REGISTRY_LOGIN_BACKOFF_MAX", "REGISTRY_LOGIN_BACKOFF_MODE"} {
		t.Setenv(key, "")
	}
}

func TestParseServeConfigLoginBackoffDefaultsAreLogOnly(t *testing.T) {
	clearLoginEnv(t)
	cfg, err := parseServeConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LoginBackoffMode != "log" || cfg.LoginBackoffThreshold != 5 || cfg.LoginBackoffBase != 2*time.Second || cfg.LoginBackoffMax != 5*time.Minute {
		t.Fatalf("defaults = %+v", cfg)
	}
}

func TestParseServeConfigLoginBackoffFlagsAndEnv(t *testing.T) {
	clearLoginEnv(t)
	t.Setenv("REGISTRY_LOGIN_BACKOFF_MODE", "enforce")
	t.Setenv("REGISTRY_LOGIN_BACKOFF_THRESHOLD", "3")
	t.Setenv("REGISTRY_LOGIN_BACKOFF_BASE", "1s")
	t.Setenv("REGISTRY_LOGIN_BACKOFF_MAX", "1m")
	cfg, err := parseServeConfig(nil)
	if err != nil || cfg.LoginBackoffMode != "enforce" || cfg.LoginBackoffThreshold != 3 || cfg.LoginBackoffBase != time.Second || cfg.LoginBackoffMax != time.Minute {
		t.Fatalf("env: %+v %v", cfg, err)
	}
	cfg, err = parseServeConfig([]string{"-login-backoff-mode=off", "-login-backoff-threshold=0"})
	if err != nil || cfg.LoginBackoffMode != "off" || cfg.LoginBackoffThreshold != 0 {
		t.Fatalf("flags override env: %+v %v", cfg, err)
	}
}

func TestParseServeConfigRejectsInvalidLoginBackoff(t *testing.T) {
	clearLoginEnv(t)
	for _, args := range [][]string{
		{"-login-backoff-mode=block"},
		{"-login-backoff-threshold=-1"},
		{"-login-backoff-base=0s"},
		{"-login-backoff-base=10s", "-login-backoff-max=1s"},
	} {
		if _, err := parseServeConfig(args); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	t.Setenv("REGISTRY_LOGIN_BACKOFF_BASE", "soon")
	if _, err := parseServeConfig(nil); err == nil || !strings.Contains(err.Error(), "REGISTRY_LOGIN_BACKOFF_BASE") {
		t.Errorf("bad env duration err = %v", err)
	}
}

func TestLoginPolicyFromConfig(t *testing.T) {
	clearLoginEnv(t)
	cfg, _ := parseServeConfig([]string{"-login-backoff-mode=enforce", "-login-backoff-threshold=0"})
	policy := loginPolicyFromConfig(cfg)
	if policy.Backoff == nil || policy.Backoff.Threshold != 0 || policy.Backoff.Mode != "enforce" {
		t.Fatalf("policy = %+v", policy.Backoff)
	}
}

func TestParseServeConfigRejectsNegativeLoginMaxConcurrency(t *testing.T) {
	t.Setenv("REGISTRY_LOGIN_MAX_CONCURRENCY", "")
	_, err := parseServeConfig([]string{"-login-max-concurrency=-1"})
	if err == nil || !strings.Contains(err.Error(), "login-max-concurrency") {
		t.Fatalf("err = %v, want a clear startup error", err)
	}
	t.Setenv("REGISTRY_LOGIN_MAX_CONCURRENCY", "-4")
	if _, err = parseServeConfig(nil); err == nil {
		t.Fatal("a negative env value must fail too")
	}
}

func TestParseServeConfigLoginBackoffModeIsCaseInsensitive(t *testing.T) {
	clearLoginEnv(t)
	for _, in := range []string{"ENFORCE", "Enforce", " enforce "} {
		cfg, err := parseServeConfig([]string{"-login-backoff-mode=" + in})
		if err != nil || cfg.LoginBackoffMode != "enforce" {
			t.Fatalf("%q -> %q, %v; want enforce", in, cfg.LoginBackoffMode, err)
		}
	}
	t.Setenv("REGISTRY_LOGIN_BACKOFF_MODE", "OFF")
	if cfg, err := parseServeConfig(nil); err != nil || cfg.LoginBackoffMode != "off" {
		t.Fatalf("env OFF -> %q, %v", cfg.LoginBackoffMode, err)
	}
	if _, err := parseServeConfig([]string{"-login-backoff-mode=Block"}); err == nil {
		t.Fatal("unknown mode must still be rejected")
	}
}

func TestParseServeConfigRefusesTrustEverythingProxies(t *testing.T) {
	t.Setenv("REGISTRY_TRUSTED_PROXIES", "")
	for _, value := range []string{"0.0.0.0/0", "::/0", "10.0.0.0/8,0.0.0.0/0", " ::/0 "} {
		_, err := parseServeConfig([]string{"-trusted-proxies", value})
		if err == nil || !strings.Contains(err.Error(), "every peer") || !strings.Contains(err.Error(), "X-Forwarded-For") {
			t.Errorf("%q: err = %v, want a refusal explaining it trusts every peer's X-Forwarded-For", value, err)
		}
	}
	for _, value := range []string{"10.0.0.0/8", "0.0.0.0/1", "::/1", "172.16.0.0/12,fd00::/8"} {
		if _, err := parseServeConfig([]string{"-trusted-proxies", value}); err != nil {
			t.Errorf("%q must be accepted: %v", value, err)
		}
	}
}
