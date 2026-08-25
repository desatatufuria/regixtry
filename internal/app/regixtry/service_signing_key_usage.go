package regixtry

import (
	"context"
	"crypto/ecdsa"
	"encoding/base64"
	"strings"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
)

// maxKeyUsageScanTags bounds CountManifestsSignedByKey's scan: this is a
// best-effort, advisory count with no persistent index to make it cheap
// (there is no "which tags does this key currently protect" table), so a
// full active-tag scan up to a bound is the deliberate compromise -- not a
// full historical/orphaned-digest crawl, and not an unbounded one either.
const maxKeyUsageScanTags = 500

// CountManifestsSignedByKey reports how many of a scope's currently-tagged
// digests verify against exactly one trusted key PEM -- a best-effort,
// non-exhaustive advisory (never a blocking gate) shown before an operator
// revokes a key. repository == "" means global scope (every repository); a
// non-empty repository scopes to just that one. capped reports whether the
// scope had more tags than maxKeyUsageScanTags, so a caller can render "at
// least N (stopped counting)" instead of implying an exact total.
//
// This is a NEW, separate, read-only advisory path -- it must never alter
// verifySignature/verifyBundleSignature's own pull-time policy-enforcement
// behavior. It reuses their core signature-lookup + signing.Verify +
// claims-check logic (via keyUsageMatchesLegacy/keyUsageMatchesBundle
// below), adapted to a single candidate key instead of a trusted-key list,
// and it tolerates (never aborts on) any single tag's unreadable/missing
// signature artifact -- one bad entry must not blank out the whole count.
func (s *Service) CountManifestsSignedByKey(ctx context.Context, repository string, keyPEM string) (count int, capped bool, err error) {
	key, err := signing.ParseTrustedKey(keyPEM)
	if err != nil {
		return 0, false, domain.NewValidationError("key is not a usable trusted key: " + err.Error())
	}
	return s.countManifestsSignedByKey(ctx, repository, key, maxKeyUsageScanTags)
}

// countManifestsSignedByKey is CountManifestsSignedByKey's implementation,
// parameterized on scanCap so a test can pin the "stop at the cap, report
// capped: true" contract with a small, fast cap instead of the real 500.
func (s *Service) countManifestsSignedByKey(ctx context.Context, repository string, key *ecdsa.PublicKey, scanCap int) (count int, capped bool, err error) {
	repositories, err := s.keyUsageScanRepositories(ctx, repository)
	if err != nil {
		return 0, false, err
	}

	scanned := 0
scan:
	for _, ref := range repositories {
		tags, err := s.metadata.ListTagsWithCreatedAt(ctx, s.tenant(ctx), ref, 0, "")
		if err != nil {
			return 0, false, err
		}
		for _, tag := range tags {
			if isCosignSignatureArtifactTag(tag.Name) {
				// A signature artifact's own accessory tag is not a
				// currently-tagged image this feature's "currently
				// verifies" framing is about (mirrors TagDetails'
				// identical exclusion, queries.go).
				continue
			}
			if scanned >= scanCap {
				capped = true
				break scan
			}
			scanned++

			manifest, resolveErr := s.metadata.ResolveManifest(ctx, s.tenant(ctx), ref, tag.Name)
			if resolveErr != nil {
				// Advisory, not authoritative: one unreadable tag must
				// never abort the whole scan, and must never itself
				// count as a match.
				continue
			}

			matched, matchErr := s.keyUsageMatches(ctx, ref, manifest.Digest.String(), key)
			if matchErr != nil || !matched {
				continue
			}
			count++
		}
	}

	return count, capped, nil
}

// keyUsageScanRepositories resolves CountManifestsSignedByKey's scope to a
// concrete repository list: every repository for global scope (""), or
// exactly one for a scoped call.
func (s *Service) keyUsageScanRepositories(ctx context.Context, repository string) ([]domain.RepositoryRef, error) {
	trimmed := strings.TrimSpace(repository)
	if trimmed == "" {
		return s.metadata.Catalog(ctx, s.tenant(ctx), 0, "")
	}
	ref, err := parseRepository(trimmed)
	if err != nil {
		return nil, err
	}
	return []domain.RepositoryRef{ref}, nil
}

// keyUsageMatches reports whether digest's signature (legacy `.sig` or
// modern bundle format) verifies against exactly this one candidate key.
// Unlike verifySignature, it never returns a policy-violation error for
// "no usable signature" outcomes -- those are simply (false, nil), the
// advisory shape this whole feature needs. Only a genuine metadata/blob
// infrastructure error propagates, and even that is swallowed by the caller
// (countManifestsSignedByKey) rather than aborting the scan.
func (s *Service) keyUsageMatches(ctx context.Context, repositoryRef domain.RepositoryRef, digest string, key *ecdsa.PublicKey) (bool, error) {
	tag, err := signing.SignatureTag(digest)
	if err != nil {
		return false, nil
	}

	sigManifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repositoryRef, tag)
	if err != nil {
		if domain.IsCode(err, domain.ErrorCodeNotFound) {
			return s.keyUsageMatchesBundle(ctx, repositoryRef, digest, key)
		}
		return false, err
	}

	entries, err := signing.ParseSignatureManifest(sigManifest.Payload)
	if err != nil || len(entries) == 0 {
		return false, nil
	}

	for _, entry := range entries {
		payload, err := s.openSignaturePayload(ctx, entry.PayloadDigest)
		if err != nil {
			return false, err
		}
		if payload == nil {
			continue
		}
		if err := signing.Verify(key, payload, entry.Signature); err != nil {
			continue
		}
		if err := signing.CheckClaims(payload, digest); err != nil {
			continue
		}
		return true, nil
	}
	return false, nil
}

// keyUsageMatchesBundle is keyUsageMatches' fallback for the modern cosign
// v3 bundle format, mirroring verifyBundleSignature's own resolution and
// DSSE verification exactly (service_signing.go), adapted to a single
// candidate key and the advisory (bool, error) shape.
func (s *Service) keyUsageMatchesBundle(ctx context.Context, repositoryRef domain.RepositoryRef, digest string, key *ecdsa.PublicKey) (bool, error) {
	indexTag, err := signing.BundleIndexTag(digest)
	if err != nil {
		return false, nil
	}

	indexManifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repositoryRef, indexTag)
	if err != nil {
		if domain.IsCode(err, domain.ErrorCodeNotFound) {
			return false, nil
		}
		return false, err
	}

	entries, err := signing.ParseBundleIndex(indexManifest.Payload)
	if err != nil {
		return false, nil
	}

	for _, entry := range entries {
		referrerManifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repositoryRef, entry.Digest)
		if err != nil {
			if domain.IsCode(err, domain.ErrorCodeNotFound) {
				continue
			}
			return false, err
		}

		subjectDigest, layerDigest, err := signing.ParseBundleReferrerManifest(referrerManifest.Payload)
		if err != nil || subjectDigest != digest || layerDigest == "" {
			continue
		}

		bundlePayload, err := s.openSignaturePayload(ctx, layerDigest)
		if err != nil {
			return false, err
		}
		if bundlePayload == nil {
			continue
		}

		bundle, err := signing.ParseBundleDocument(bundlePayload)
		if err != nil {
			continue
		}

		payload, err := base64.StdEncoding.DecodeString(bundle.Payload)
		if err != nil {
			continue
		}

		paeBytes := signing.PAE(bundle.PayloadType, payload)
		for _, signatureB64 := range bundle.Signatures {
			if err := signing.Verify(key, paeBytes, signatureB64); err != nil {
				continue
			}
			if err := signing.CheckBundleClaims(payload, digest); err != nil {
				continue
			}
			return true, nil
		}
	}

	return false, nil
}
