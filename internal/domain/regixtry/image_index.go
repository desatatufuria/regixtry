package regixtry

import (
	"encoding/json"
	"fmt"
)

const (
	// OCIImageIndexMediaType is the OCI 1.1 Image Index media type: a
	// multi-architecture manifest list keyed by manifests[].platform.
	OCIImageIndexMediaType = "application/vnd.oci.image.index.v1+json"

	// DockerManifestListMediaType is Docker's pre-OCI equivalent of
	// OCIImageIndexMediaType -- same manifests[]/platform shape. Kept as a
	// distinct constant (rather than aliased away) because real-world
	// clients, and this registry's own push path
	// (internal/app/regixtry/service.go's manifestEnvelope), still encounter
	// it verbatim.
	DockerManifestListMediaType = "application/vnd.docker.distribution.manifest.list.v2+json"

	// MaxImageIndexBytes bounds how large a payload ParseImageIndexEntries
	// will parse. This is a read path (Console manifest inspection), not a
	// pusher-facing verification hot path the way
	// internal/domain/signing/bundle.go's MaxBundleManifestBytes is, but the
	// same defensive bound is free and keeps a pathological stored payload
	// from producing an unbounded parse.
	MaxImageIndexBytes = 256 << 10

	// MaxImageIndexEntries bounds the number of manifests[] entries parsed,
	// capping the per-inspection loop -- mirrors
	// internal/domain/signing/bundle.go's MaxBundleIndexEntries reasoning.
	MaxImageIndexEntries = 256
)

// Platform is one child manifest's target platform, read verbatim from an
// OCI Image Index / Docker Manifest List manifests[] entry's "platform"
// object. Variant is "" when the entry declares none (most non-ARM
// platforms).
type Platform struct {
	Architecture string
	OS           string
	Variant      string
}

// ImageIndexEntry is one manifests[] entry of an OCI Image Index / Docker
// Manifest List: the child manifest's own descriptor (digest/mediaType/size)
// plus its target Platform, when the entry declares one. Platform is nil for
// an entry with no "platform" object at all (legal per OCI 1.1, e.g. some
// attestation-only entries) -- distinct from a Platform whose fields are
// merely empty strings.
type ImageIndexEntry struct {
	Digest    string
	MediaType string
	Size      int64
	Platform  *Platform
}

// IsImageIndexMediaType reports whether mediaType is one of the two
// manifests[]/platform-shaped index media types ParseImageIndexEntries
// understands. Callers use this to decide whether a manifest is index-shaped
// before calling ParseImageIndexEntries at all.
func IsImageIndexMediaType(mediaType string) bool {
	return mediaType == OCIImageIndexMediaType || mediaType == DockerManifestListMediaType
}

// imageIndexEnvelope is the minimal OCI Image Index / Docker Manifest List
// shape ParseImageIndexEntries reads. Deliberately separate from
// internal/domain/signing/bundle.go's own bundleIndexEnvelope: both read the
// same OCI Image Index outer shape, but for different consumers and
// different per-entry fields -- that one reads "artifactType" for bundle
// (cosign) referrer discovery on the signature-verification path, this one
// reads "platform" for the Console manifest inspection screen's multi-arch
// breakdown. The two parsers must stay independent; neither wraps the
// other.
type imageIndexEnvelope struct {
	SchemaVersion int                       `json:"schemaVersion"`
	MediaType     string                    `json:"mediaType"`
	Manifests     []imageIndexManifestEntry `json:"manifests"`
}

type imageIndexManifestEntry struct {
	MediaType string              `json:"mediaType"`
	Digest    string              `json:"digest"`
	Size      int64               `json:"size"`
	Platform  *imageIndexPlatform `json:"platform"`
}

type imageIndexPlatform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant,omitempty"`
}

// ParseImageIndexEntries reads an OCI Image Index / Docker Manifest List
// payload's manifests[] array into ImageIndexEntry values, in payload order.
// The returned slice is never nil (mirrors
// internal/app/regixtry/queries.go's ReferrersIndex.Manifests never-nil
// discipline). Callers are expected to have already confirmed the parent
// manifest's own MediaType is index-shaped (IsImageIndexMediaType); this
// function does not re-check payload.mediaType itself -- exactly like
// ParseBundleIndex, which trusts its caller the same way.
func ParseImageIndexEntries(payload []byte) ([]ImageIndexEntry, error) {
	if len(payload) > MaxImageIndexBytes {
		return nil, NewInvalidManifestError(fmt.Sprintf("image index exceeds %d bytes", MaxImageIndexBytes))
	}

	var envelope imageIndexEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, NewInvalidManifestError("image index is not valid JSON")
	}

	if len(envelope.Manifests) > MaxImageIndexEntries {
		return nil, NewInvalidManifestError(fmt.Sprintf("image index declares more than %d manifests", MaxImageIndexEntries))
	}

	entries := make([]ImageIndexEntry, 0, len(envelope.Manifests))
	for _, manifest := range envelope.Manifests {
		entry := ImageIndexEntry{
			Digest:    manifest.Digest,
			MediaType: manifest.MediaType,
			Size:      manifest.Size,
		}
		if manifest.Platform != nil {
			entry.Platform = &Platform{
				Architecture: manifest.Platform.Architecture,
				OS:           manifest.Platform.OS,
				Variant:      manifest.Platform.Variant,
			}
		}
		entries = append(entries, entry)
	}

	return entries, nil
}
