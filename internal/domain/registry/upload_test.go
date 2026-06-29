package registry

import (
	"testing"
	"time"
)

func TestUploadStateValidate(t *testing.T) {
	t.Parallel()

	upload := UploadState{
		ID:         "upload-1",
		Repository: MustParseRepositoryRef("library/alpine"),
		Status:     UploadStatusActive,
		Size:       10,
		StartedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Location:   "/tmp/upload-1/data",
	}

	if err := upload.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestDescriptorValidateRejectsNegativeSize(t *testing.T) {
	t.Parallel()

	err := Descriptor{Digest: DigestFromBytes([]byte("blob")), Size: -1}.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}

	if !IsCode(err, ErrorCodeValidation) {
		t.Fatalf("expected validation error code, got %v", err)
	}
}
