package regixtry

import (
	"context"
	"errors"
	"strings"
	"testing"

	domain "regixtry/internal/domain/regixtry"
)

func tagValidationPayload(configDigest string) []byte {
	return []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + configDigest + `","size":3}}`)
}

// A reference becomes a stored tag name that the admin TUI and every other
// client draw back. Control characters (C0, DEL, C1, line and paragraph
// separators) are refused at publish time; the tag grammar is NOT enforced,
// so every ordinary tag keeps working exactly as before.
func TestParseManifestPayloadRefusesControlCharactersInTheReference(t *testing.T) {
	t.Parallel()

	payload := tagValidationPayload(digestForTest([]byte("cfg")))
	for _, tt := range []struct {
		name      string
		reference string
	}{
		{"interior newline", "x\n\n\n\ny"},
		{"interior carriage return", "x\ry"},
		{"interior tab", "x\ty"},
		{"escape sequence", "x\x1b[2Jy"},
		{"nul", "x\x00y"},
		{"del", "x\x7fy"},
		{"c1 control", "x\u009by"},
		{"line separator", "x y"},
		{"paragraph separator", "x y"},
		{"next line", "x\u0085y"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, tag, err := parseManifestPayload(tt.reference, "", payload)
			var registryErr *domain.Error
			if !errors.As(err, &registryErr) || registryErr.Code != domain.ErrorCodeValidation {
				t.Fatalf("parseManifestPayload(%q) error = %v, want a validation error", tt.reference, err)
			}
			if tag != "" || strings.ContainsAny(registryErr.Message, "\n\r\x1b") {
				t.Fatalf("tag = %q, message = %q: must name no tag and echo no control", tag, registryErr.Message)
			}
		})
	}
}

func TestParseManifestPayloadKeepsAcceptingOrdinaryReferences(t *testing.T) {
	t.Parallel()

	payload := tagValidationPayload(digestForTest([]byte("cfg")))
	manifest, _, err := parseManifestPayload("latest", "", payload)
	if err != nil {
		t.Fatalf("parseManifestPayload(latest) error = %v", err)
	}
	digest := manifest.Digest.String()

	for _, tt := range []struct {
		reference string
		wantTag   string
	}{
		{"latest", "latest"},
		{"v1.0.0", "v1.0.0"},
		{"1.0", "1.0"},
		{"release_2026-11-23", "release_2026-11-23"},
		{"sha256-" + strings.Repeat("a", 64) + ".sig", "sha256-" + strings.Repeat("a", 64) + ".sig"},
		{"sha256-" + strings.Repeat("a", 64), "sha256-" + strings.Repeat("a", 64)},
		{"  padded  ", "padded"},
		{"日本語", "日本語"},
		{"with space", "with space"},
		{"x​y", "x​y"}, // not a control: the grammar is deliberately not enforced
		{digest, ""},
	} {
		_, tag, err := parseManifestPayload(tt.reference, "", payload)
		if err != nil || tag != tt.wantTag {
			t.Fatalf("parseManifestPayload(%q) = (%q, %v), want (%q, nil)", tt.reference, tag, err, tt.wantTag)
		}
	}

	// A digest that does not match still reports the mismatch, not a validation error.
	if _, _, err := parseManifestPayload("sha256:"+strings.Repeat("0", 64), "", payload); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("mismatching digest error = %v, want digest mismatch", err)
	}
}

func TestServicePublishManifestRefusesTagWithNewlineAndAcceptsOrdinaryTags(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	ctx := context.Background()

	upload, err := service.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := service.AppendUpload(ctx, "library/alpine", upload.ID, strings.NewReader("layer-one")); err != nil {
		t.Fatalf("AppendUpload() error = %v", err)
	}
	blob, err := service.CompleteUpload(ctx, "library/alpine", upload.ID, digestForTest([]byte("layer-one")), nil)
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	payload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + blob.Digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + blob.Digest + `","size":9}]}`)
	const mediaType = "application/vnd.oci.image.manifest.v1+json"

	hostile := "x\n\n\n\n\n\n\n\ny"
	if _, err := service.PublishManifest(ctx, "library/alpine", hostile, mediaType, payload); err == nil {
		t.Fatalf("PublishManifest(%q) succeeded, want a validation error", hostile)
	}

	for _, tag := range []string{"latest", "v1.0.0"} {
		if _, err := service.PublishManifest(ctx, "library/alpine", tag, mediaType, payload); err != nil {
			t.Fatalf("PublishManifest(%q) error = %v", tag, err)
		}
	}

	tags, err := service.TagDetails(ctx, "library/alpine", 10, "")
	if err != nil {
		t.Fatalf("TagDetails() error = %v", err)
	}
	var names []string
	for _, tag := range tags {
		names = append(names, tag.Name)
	}
	if strings.Join(names, ",") != "latest,v1.0.0" && strings.Join(names, ",") != "v1.0.0,latest" {
		t.Fatalf("stored tags = %q, want only latest and v1.0.0", names)
	}
}
