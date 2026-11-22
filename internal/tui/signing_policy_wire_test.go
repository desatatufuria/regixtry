package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	appauth "regixtry/internal/app/auth"
	appregixtry "regixtry/internal/app/regixtry"
	authpostgres "regixtry/internal/infra/auth/postgres"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
	regixtryhttp "regixtry/internal/protocol/http"
)

type signingWireEnv struct {
	service *appregixtry.Service
	client  *HTTPAdminClient
	session AdminSession
	ctx     context.Context
}

func signingWireSetup(t *testing.T) signingWireEnv {
	dir := t.TempDir()
	blob, _ := fsblob.New(filepath.Join(dir, "c"))
	meta, err := metadata.New(filepath.Join(dir, "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { meta.Close() })
	authStore, err := authpostgres.NewWithDriver("sqlite", filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { authStore.Close() })
	access := ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"})
	authService := appauth.NewService(authStore)
	service := appregixtry.NewService(blob, meta, access, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	router := regixtryhttp.NewRouter(service, authService)
	ctx := context.Background()
	if _, err := authService.BootstrapAdmin(ctx, ports.BootstrapAdminInput{Username: "admin", Password: "fixture-pass-9x"}); err != nil {
		t.Fatal(err)
	}
	login, err := authService.LoginWithPassword(ctx, "admin", "fixture-pass-9x", nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client, _ := NewHTTPAdminClient(server.URL, &http.Client{Timeout: 10 * time.Second})
	return signingWireEnv{service, client, AdminSession{Username: "admin", BearerToken: login.BearerToken, ExpiresAt: time.Now().Add(time.Hour)}, ctx}
}

func signingWireKey(t *testing.T) string {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func signingWireModel(t *testing.T, e signingWireEnv, to screen, w, h int) (Model, tea.Cmd) {
	m := newAdminReadyModel(t, e.client)
	m = resizeModel(t, m, w, h)
	m.adminSession, m.adminAuth = e.session, adminAuthStateAuthenticated
	updated, cmd := m.Update(navigateMsg{To: to})
	return updated.(Model), cmd
}

func TestSigningPolicySaveRoundTripsThroughTheRealServer(t *testing.T) {
	e := signingWireSetup(t)
	k1, k2 := signingWireKey(t), signingWireKey(t)
	id := ports.TrustedIdentity{CertificateIdentityRegexp: "^dev@example.com$", CertificateOIDCIssuer: "https://issuer.example"}
	seed := ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{k1, k2}, TrustedIdentities: []ports.TrustedIdentity{id}, UnsignedSelfRead: "pusher"}
	if _, err := e.service.UpdateSigningPolicySettings(e.ctx, seed); err != nil {
		t.Fatal(err)
	}
	m, cmd := signingWireModel(t, e, screenSecuritySigningConfig, 150, 24)
	m = repoAccessRun(t, m, cmd)
	m = repoAccessPress(t, m, testKeyRunes("p"), testKeyEnter())
	got, _ := e.service.GetSigningPolicySettings(e.ctx)
	t.Logf("after plain save: enabled=%v keys=%d identities=%d mode=%q", got.Enabled, len(got.TrustedPublicKeys), len(got.TrustedIdentities), got.UnsignedSelfRead)
	if len(got.TrustedIdentities) != 1 || got.UnsignedSelfRead != "pusher" || len(got.TrustedPublicKeys) != 2 || !reflect.DeepEqual(got.Enabled, true) {
		t.Fatalf("SAVE ALTERED POLICY: %+v", got)
	}
}

// A no-op save sends the loaded policy back and the stored policy is the same
// afterwards: keys in their order, identities, mode and enabled flag.
func TestSigningPolicyNoOpSaveLeavesTheStoredPolicyUnchanged(t *testing.T) {
	e := signingWireSetup(t)
	keys := []string{signingWireKey(t), signingWireKey(t), signingWireKey(t)}
	ids := []ports.TrustedIdentity{
		{CertificateIdentityRegexp: "^b@example.com$", CertificateOIDCIssuer: "https://issuer.b"},
		{CertificateIdentityRegexp: "^a@example.com$", CertificateOIDCIssuer: "https://issuer.a"},
	}
	seed := ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: keys, TrustedIdentities: ids, UnsignedSelfRead: "repo_push"}
	if _, err := e.service.UpdateSigningPolicySettings(e.ctx, seed); err != nil {
		t.Fatal(err)
	}
	before, _ := e.service.GetSigningPolicySettings(e.ctx)

	m, cmd := signingWireModel(t, e, screenSecuritySigningConfig, 150, 24)
	m = repoAccessRun(t, m, cmd)
	m = repoAccessPress(t, m, testKeyRunes("p"), testKeyEnter())

	after, _ := e.service.GetSigningPolicySettings(e.ctx)
	if after.Enabled != before.Enabled || after.UnsignedSelfRead != before.UnsignedSelfRead ||
		!reflect.DeepEqual(after.TrustedPublicKeys, before.TrustedPublicKeys) || !reflect.DeepEqual(after.TrustedIdentities, before.TrustedIdentities) {
		t.Fatalf("a no-op save changed the policy:\nbefore %+v\nafter  %+v", before, after)
	}
}

// Changes made in the modal persist: the mode and a new identity.
func TestSigningPolicyEditsPersistThroughTheRealServer(t *testing.T) {
	e := signingWireSetup(t)
	seed := ports.SigningPolicySettings{Enabled: true, TrustedPublicKeys: []string{signingWireKey(t)}, UnsignedSelfRead: "pusher"}
	if _, err := e.service.UpdateSigningPolicySettings(e.ctx, seed); err != nil {
		t.Fatal(err)
	}
	m, cmd := signingWireModel(t, e, screenSecuritySigningConfig, 200, 40)
	m = repoAccessRun(t, m, cmd)
	// Space on the self-read field cycles pusher to repo_push; Tab x3 reaches
	// the identities, n adds one.
	m = repoAccessPress(t, m, testKeyRunes("p"), testKeyTab(), testKeySpace(), testKeyTab(), testKeyTab(), testKeyRunes("n"))
	m = repoAccessPress(t, m, repoAccessType("^dev@example.com$")...)
	m = repoAccessPress(t, m, testKeyTab())
	m = repoAccessPress(t, m, repoAccessType("https://issuer.example")...)
	m = repoAccessPress(t, m, testKeyEnter(), testKeyEnter())

	got, _ := e.service.GetSigningPolicySettings(e.ctx)
	if got.UnsignedSelfRead != "repo_push" || len(got.TrustedIdentities) != 1 || got.TrustedIdentities[0].CertificateIdentityRegexp != "^dev@example.com$" || len(got.TrustedPublicKeys) != 1 {
		t.Fatalf("stored policy = %+v, want repo_push, the new identity and the original key", got)
	}
}
