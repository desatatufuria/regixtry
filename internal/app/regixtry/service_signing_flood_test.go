package regixtry

import (
	"context"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
)

// The tests in this file pin the audit run-2 lead: a repository writer who can
// push referrers must not be able to deny verification of a legitimately
// signed digest by flooding it with copies or variants of valid-looking
// bundles (S3).

// floodReferrerManifest returns a bundle referrer manifest for subject whose
// single layer is the bundle document layerDoc. salt varies the manifest digest
// without touching the bundle bytes (a different annotation).
func floodReferrerManifest(t *testing.T, subject string, layerDoc []byte, salt string) ([]byte, string) {
	t.Helper()

	emptyConfig := []byte("{}")
	raw, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config":        map[string]any{"mediaType": "application/vnd.oci.empty.v1+json", "digest": digestForTest(emptyConfig), "size": len(emptyConfig)},
		"layers":        []map[string]any{{"mediaType": signing.SigstoreBundleMediaType, "digest": digestForTest(layerDoc), "size": len(layerDoc)}},
		"subject":       map[string]any{"mediaType": "application/vnd.docker.distribution.manifest.v2+json", "digest": subject, "size": 949},
		"artifactType":  signing.SigstoreBundleMediaType,
		"annotations":   map[string]string{"salt": salt},
	})
	if err != nil {
		t.Fatalf("json.Marshal(referrer) error = %v", err)
	}
	return raw, digestForTest(raw)
}

func publishFloodReferrer(t *testing.T, service *Service, repository string, manifest []byte, digest string) {
	t.Helper()
	if _, err := service.PublishManifest(context.Background(), repository, digest, "application/vnd.oci.image.manifest.v1+json", manifest); err != nil {
		t.Fatalf("PublishManifest(referrer) error = %v", err)
	}
}

// replayWrapperVariant returns a bundle document carrying the same DSSE
// envelope (so the same payload and signature) in a different wrapper.
func replayWrapperVariant(t *testing.T, bundleDoc []byte, i int) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(bundleDoc, &doc); err != nil {
		t.Fatalf("json.Unmarshal(bundle) error = %v", err)
	}
	doc["wrapperVariant"] = i
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal(variant) error = %v", err)
	}
	return raw
}

// garbageBundleClaiming returns a distinct bundle document whose payload binds
// digestHex but whose signature is junk, so only cryptography can reject it.
func garbageBundleClaiming(t *testing.T, digestHex string, i int) []byte {
	t.Helper()
	return bundleDocWithSignatures(t, digestHex, []string{base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("junk-%d", i)))})
}

// workProbe returns a context that counts verification work units.
func workProbe() (context.Context, *atomic.Int64) {
	var units atomic.Int64
	ctx := context.WithValue(context.Background(), signingWorkProbeKey{}, func(n int) { units.Add(int64(n)) })
	return ctx, &units
}

func TestServiceVerifySignature_ReplayedTrustedBundleFloodDoesNotDenySignedDigest(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - replay flood")
	other := seedArbitraryImageManifest(t, service, repository, " - replay flood, other image")
	key, keyPEM := generateTestECDSAP256KeyPair(t)

	seedReferrerOnlyBundleSignatureArtifact(t, service, repository, digest, bareHex(digest), key)
	genuineDigests := referrerDigestsForTest(t, service, repository, digest)
	if len(genuineDigests) != 1 {
		t.Fatalf("setup: %d genuine referrers, want 1", len(genuineDigests))
	}

	// The public bundle signed for ANOTHER image, replayed by a writer in more
	// than maxSignatureCandidates referrers of this digest. ListReferrers orders
	// by digest, so keep only the ones sorting BEFORE the genuine referrer:
	// at the base commit that deterministically exhausts the cap first.
	replay := buildBundleDocument(t, key, bundleInTotoStatementPayload(t, bareHex(other)))
	uploadBlobForTest(t, service, repository, replay)
	uploadBlobForTest(t, service, repository, []byte("{}"))
	published := 0
	for i := 0; published < maxSignatureCandidates+1; i++ {
		manifest, manifestDigest := floodReferrerManifest(t, digest, replay, fmt.Sprintf("replay-%d", i))
		if manifestDigest >= genuineDigests[0] {
			continue
		}
		publishFloodReferrer(t, service, repository, manifest, manifestDigest)
		published++
	}
	service.WaitForBackgroundWork()

	state, match, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
	if err != nil || state != signatureStateVerified {
		t.Fatalf("verifySignature() = (%q, %v), want verified despite %d replayed referrers", state, err, published)
	}
	if match.KeyFingerprint == "" {
		t.Fatalf("verifySignature() match = %+v, want the trusted key fingerprint", match)
	}
}

func TestServiceVerifySignature_ReplayFloodAloneNeverMakesUnsignedDigestSigned(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - unsigned flood")
	other := seedArbitraryImageManifest(t, service, repository, " - unsigned flood, other image")
	key, keyPEM := generateTestECDSAP256KeyPair(t)

	replay := buildBundleDocument(t, key, bundleInTotoStatementPayload(t, bareHex(other)))
	uploadBlobForTest(t, service, repository, replay)
	uploadBlobForTest(t, service, repository, []byte("{}"))
	for i := 0; i < maxSignatureCandidates+50; i++ {
		manifest, manifestDigest := floodReferrerManifest(t, digest, replay, fmt.Sprintf("replay-%d", i))
		publishFloodReferrer(t, service, repository, manifest, manifestDigest)
	}
	service.WaitForBackgroundWork()

	state, _, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
	requirePolicyDenial(t, state, err, signatureStateMismatched)
}

func TestServiceVerifySignature_OrdinaryCasesKeepTheirStatus(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		seed  func(t *testing.T, service *Service, repository, digest, other string, key, attacker *ecdsa.PrivateKey)
		want  string
		allow bool
	}{
		{name: "no referrers is unsigned", seed: func(*testing.T, *Service, string, string, string, *ecdsa.PrivateKey, *ecdsa.PrivateKey) {}, want: signatureStateUnsigned},
		{name: "one genuine referrer verifies", allow: true, want: signatureStateVerified, seed: func(t *testing.T, s *Service, r, d, o string, k, a *ecdsa.PrivateKey) {
			seedReferrerOnlyBundleSignatureArtifact(t, s, r, d, bareHex(d), k)
		}},
		{name: "one untrusted signer is untrusted", want: signatureStateUntrusted, seed: func(t *testing.T, s *Service, r, d, o string, k, a *ecdsa.PrivateKey) {
			seedReferrerOnlyBundleSignatureArtifact(t, s, r, d, bareHex(d), a)
		}},
		{name: "one replayed signature is mismatched", want: signatureStateMismatched, seed: func(t *testing.T, s *Service, r, d, o string, k, a *ecdsa.PrivateKey) {
			seedReferrerOnlyBundleSignatureArtifact(t, s, r, d, bareHex(o), k)
		}},
		{name: "a few mixed referrers still verify", allow: true, want: signatureStateVerified, seed: func(t *testing.T, s *Service, r, d, o string, k, a *ecdsa.PrivateKey) {
			seedReferrerOnlyBundleSignatureArtifact(t, s, r, d, bareHex(o), k)
			seedReferrerOnlyBundleSignatureArtifact(t, s, r, d, bareHex(d), a)
			seedReferrerOnlyBundleSignatureArtifact(t, s, r, d, bareHex(d), k)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service, cleanup := newTestService(t, allowAllAccessController{})
			defer cleanup()

			repository := "library/alpine"
			digest := seedArbitraryImageManifest(t, service, repository, " - ordinary "+tc.name)
			other := seedArbitraryImageManifest(t, service, repository, " - ordinary other "+tc.name)
			key, keyPEM := generateTestECDSAP256KeyPair(t)
			attacker, _ := generateTestECDSAP256KeyPair(t)
			tc.seed(t, service, repository, digest, other, key, attacker)

			state, _, err := verifyForTest(t, service, repository, digest, []string{keyPEM})
			if tc.allow {
				if err != nil || state != tc.want {
					t.Fatalf("verifySignature() = (%q, %v), want %q", state, err, tc.want)
				}
				return
			}
			requirePolicyDenial(t, state, err, tc.want)
		})
	}
}

// floodFixture uploads the bundle blobs a flood needs and returns the
// candidate manifests in a caller-controlled order.
type floodFixture struct {
	service  *Service
	digest   string
	keyPEM   string
	genuine  []byte
	flood    [][]byte
	keyEntry []trustedKeyEntry
}

func newFloodFixture(t *testing.T, kind string, n int) *floodFixture {
	t.Helper()

	service, cleanup := newTestService(t, allowAllAccessController{})
	t.Cleanup(cleanup)

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - flood "+kind)
	other := seedArbitraryImageManifest(t, service, repository, " - flood other "+kind)
	key, keyPEM := generateTestECDSAP256KeyPair(t)

	genuineDoc := buildBundleDocument(t, key, bundleInTotoStatementPayload(t, bareHex(digest)))
	uploadBlobForTest(t, service, repository, genuineDoc)
	genuine, _ := floodReferrerManifest(t, digest, genuineDoc, "genuine")

	replayDoc := buildBundleDocument(t, key, bundleInTotoStatementPayload(t, bareHex(other)))
	uploadBlobForTest(t, service, repository, replayDoc)

	fx := &floodFixture{service: service, digest: digest, keyPEM: keyPEM, genuine: genuine, keyEntry: parseTrustedKeys([]string{keyPEM})}
	for i := 0; i < n; i++ {
		var doc []byte
		switch kind {
		case "identical-replay":
			doc = replayDoc
		case "wrapper-variants":
			doc = replayWrapperVariant(t, replayDoc, i)
			uploadBlobForTest(t, service, repository, doc)
		case "garbage-claiming-digest":
			doc = garbageBundleClaiming(t, bareHex(digest), i)
			uploadBlobForTest(t, service, repository, doc)
		case "garbage-claiming-other":
			doc = garbageBundleClaiming(t, bareHex(other), i)
			uploadBlobForTest(t, service, repository, doc)
		default:
			t.Fatalf("unknown flood kind %q", kind)
		}
		manifest, _ := floodReferrerManifest(t, digest, doc, fmt.Sprintf("%s-%d", kind, i))
		fx.flood = append(fx.flood, manifest)
	}
	return fx
}

// order returns the candidates with the genuine one at position pos (clamped).
func (fx *floodFixture) order(pos int) [][]byte {
	if pos > len(fx.flood) {
		pos = len(fx.flood)
	}
	out := make([][]byte, 0, len(fx.flood)+1)
	out = append(out, fx.flood[:pos]...)
	out = append(out, fx.genuine)
	return append(out, fx.flood[pos:]...)
}

func TestServiceEvaluateBundleCandidates_FloodNeverStarvesGenuineSignature(t *testing.T) {
	t.Parallel()

	const n = 1000
	for _, kind := range []string{"identical-replay", "wrapper-variants", "garbage-claiming-digest", "garbage-claiming-other"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			fx := newFloodFixture(t, kind, n)
			for _, pos := range []int{0, n / 2, n} {
				ctx, units := workProbe()
				state, match, err := fx.service.evaluateBundleCandidates(ctx, fx.digest, fx.order(pos), fx.keyEntry, nil)
				if err != nil || state != signatureStateVerified {
					t.Fatalf("genuine at %d: state = (%q, %v), want verified", pos, state, err)
				}
				if match.KeyFingerprint == "" {
					t.Fatalf("genuine at %d: match = %+v, want fingerprint", pos, match)
				}
				if units.Load() > maxBundleCryptoWork {
					t.Fatalf("genuine at %d: %d verifications, bound %d", pos, units.Load(), maxBundleCryptoWork)
				}
				if kind != "garbage-claiming-digest" && units.Load() > 1 {
					t.Fatalf("genuine at %d: %d verifications, want at most 1 (flood must cost no cryptography)", pos, units.Load())
				}
			}
		})
	}
}

func TestServiceEvaluateBundleCandidates_FloodWithoutGenuineStaysDeniedAndBounded(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		kind string
		want string
	}{
		{kind: "identical-replay", want: signatureStateMismatched},
		{kind: "wrapper-variants", want: signatureStateMismatched},
		{kind: "garbage-claiming-digest", want: ""},
		{kind: "garbage-claiming-other", want: ""},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()

			fx := newFloodFixture(t, tc.kind, 1000)
			ctx, units := workProbe()
			state, _, err := fx.service.evaluateBundleCandidates(ctx, fx.digest, fx.flood, fx.keyEntry, nil)
			if err != nil || state != tc.want || state == signatureStateVerified {
				t.Fatalf("state = (%q, %v), want %q and never verified", state, err, tc.want)
			}
			if units.Load() > maxBundleCryptoWork+int64(maxSignatureCandidates*len(fx.keyEntry)) {
				t.Fatalf("%d verifications exceed the bound", units.Load())
			}
		})
	}
}

func TestServiceVerifySignature_TooManyCandidatesIsAnHonestUnverifiableNeverSigned(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, " - too many")
	_, keyPEM := generateTestECDSAP256KeyPair(t)
	uploadBlobForTest(t, service, repository, []byte("{}"))
	for i := 0; i < maxBundleCandidatesInspected+1; i++ {
		doc := garbageBundleClaiming(t, bareHex(digest), i)
		uploadBlobForTest(t, service, repository, doc)
		manifest, manifestDigest := floodReferrerManifest(t, digest, doc, fmt.Sprintf("many-%d", i))
		publishFloodReferrer(t, service, repository, manifest, manifestDigest)
	}
	service.WaitForBackgroundWork()

	ctx, units := workProbe()
	state, _, err := service.verifySignature(ctx, repository, digest, signingPolicyForTest(t, true, []string{keyPEM}))
	requirePolicyDenial(t, state, err, signatureStateUnverifiable)
	if !strings.Contains(err.Error(), "too many candidates") {
		t.Fatalf("error = %v, want it to say %q", err, "too many candidates")
	}
	if units.Load() > maxBundleCryptoWork {
		t.Fatalf("%d verifications, bound %d", units.Load(), maxBundleCryptoWork)
	}
}

func TestServiceEvaluateBundleCandidates_BeyondInspectionBoundIsUnverifiableNotSigned(t *testing.T) {
	t.Parallel()

	fx := newFloodFixture(t, "garbage-claiming-other", maxBundleCandidatesInspected+10)
	manifests := fx.order(len(fx.flood)) // genuine last, past the bound
	state, _, err := fx.service.evaluateBundleCandidates(context.Background(), fx.digest, manifests, fx.keyEntry, nil)
	if err != nil || state != signatureStateUnverifiable {
		t.Fatalf("state = (%q, %v), want unverifiable", state, err)
	}
}

func TestServiceEvaluateBundleCandidates_UnsignedDigestNeverBecomesSigned(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"identical-replay", "wrapper-variants", "garbage-claiming-digest", "garbage-claiming-other"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			fx := newFloodFixture(t, kind, 300)
			for _, manifests := range [][][]byte{fx.flood, reversed(fx.flood)} {
				state, _, err := fx.service.evaluateBundleCandidates(context.Background(), fx.digest, manifests, fx.keyEntry, nil)
				if err != nil || state == signatureStateVerified {
					t.Fatalf("state = (%q, %v): a flood without a genuine signature must never verify", state, err)
				}
			}
		})
	}
}

func TestServiceEvaluateBundleCandidates_ConcurrentVerificationsAgree(t *testing.T) {
	t.Parallel()

	fx := newFloodFixture(t, "wrapper-variants", 200)
	manifests := fx.order(150)

	var wg sync.WaitGroup
	results := make([]string, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state, _, err := fx.service.evaluateBundleCandidates(context.Background(), fx.digest, manifests, fx.keyEntry, nil)
			if err != nil {
				state = "error: " + err.Error()
			}
			results[i] = state
		}()
	}
	wg.Wait()
	for i, got := range results {
		if got != signatureStateVerified {
			t.Fatalf("goroutine %d: state = %q, want verified", i, got)
		}
	}
}

func reversed(in [][]byte) [][]byte {
	out := append([][]byte(nil), in...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// referrerDigestsForTest lists the manifest digests of subject's referrers.
func referrerDigestsForTest(t *testing.T, service *Service, repository, subject string) []string {
	t.Helper()

	ref, err := parseRepository(repository)
	if err != nil {
		t.Fatalf("parseRepository() error = %v", err)
	}
	parsed, err := domain.ParseDigest(subject)
	if err != nil {
		t.Fatalf("ParseDigest() error = %v", err)
	}
	rows, err := service.metadata.ListReferrers(context.Background(), service.tenant(context.Background()), ref, parsed)
	if err != nil {
		t.Fatalf("ListReferrers() error = %v", err)
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Digest.String())
	}
	return out
}
