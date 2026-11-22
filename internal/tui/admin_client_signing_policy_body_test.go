package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"regixtry/internal/ports"
)

// The global signing policy PUT fully replaces the stored policy, so the body
// must carry every field the form edits.
func TestUpdateSigningPolicySendsIdentitiesAndUnsignedSelfRead(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"enabled":true,"trusted_public_keys":[],"trusted_identities":[],"updated_at":"2026-01-01T00:00:00Z"}`))
	}))
	defer server.Close()
	client, err := NewHTTPAdminClient(server.URL, &http.Client{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.UpdateSigningPolicy(context.Background(), AdminSession{BearerToken: "t"}, ports.SigningPolicySettings{
		Enabled:           true,
		TrustedIdentities: []ports.TrustedIdentity{{CertificateIdentityRegexp: "^a$", CertificateOIDCIssuer: "https://i"}},
		UnsignedSelfRead:  "pusher",
	})
	if err != nil {
		t.Fatal(err)
	}
	if body["unsigned_self_read"] != "pusher" {
		t.Errorf("unsigned_self_read missing from body: %#v", body)
	}
	if ids, _ := body["trusted_identities"].([]any); len(ids) != 1 {
		t.Errorf("trusted_identities missing from body: %#v", body)
	}
}

// TestUpdateSigningPolicyBodyCarriesExactlyTheFieldsThePutReplaces pins the
// request schema: the server decodes enabled, trusted_public_keys,
// trusted_identities and unsigned_self_read (updated_at is the server's), with
// unknown fields refused, so the body has these four and nothing else.
func TestUpdateSigningPolicyBodyCarriesExactlyTheFieldsThePutReplaces(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client, err := NewHTTPAdminClient(server.URL, &http.Client{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.UpdateSigningPolicy(context.Background(), AdminSession{BearerToken: "t"}, ports.SigningPolicySettings{}); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"enabled", "trusted_public_keys", "trusted_identities", "unsigned_self_read"} {
		if _, ok := body[field]; !ok {
			t.Errorf("body lacks %q: %#v", field, body)
		}
	}
	if len(body) != 4 {
		t.Errorf("body = %#v, want exactly the four fields the PUT replaces", body)
	}
}
