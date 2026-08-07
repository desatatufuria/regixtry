package fsblob

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	domain "regixtry/internal/domain/regixtry"
)

type Store struct {
	rootDir string
}

func New(rootDir string) (*Store, error) {
	store := &Store{rootDir: rootDir}
	for _, path := range []string{store.blobsRoot(), store.uploadsRoot()} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return nil, fmt.Errorf("create storage path %q: %w", path, err)
		}
	}

	return store, nil
}

func (s *Store) BeginUpload(_ context.Context, repository domain.RepositoryRef) (domain.UploadState, error) {
	if err := repository.Validate(); err != nil {
		return domain.UploadState{}, err
	}

	uploadID, err := newUploadID()
	if err != nil {
		return domain.UploadState{}, err
	}

	uploadDir := s.uploadDir(uploadID)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return domain.UploadState{}, err
	}

	filePath := filepath.Join(uploadDir, "data")
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	if err != nil {
		return domain.UploadState{}, err
	}
	if err := file.Close(); err != nil {
		return domain.UploadState{}, err
	}

	now := time.Now().UTC()
	state := domain.UploadState{
		ID:         uploadID,
		Repository: repository,
		Status:     domain.UploadStatusActive,
		Size:       0,
		StartedAt:  now,
		UpdatedAt:  now,
		Location:   filePath,
	}

	if err := s.writeUploadState(state); err != nil {
		return domain.UploadState{}, err
	}

	return state, nil
}

func (s *Store) GetUpload(_ context.Context, uploadID string) (domain.UploadState, error) {
	state, err := s.readUploadState(uploadID)
	if err != nil {
		return domain.UploadState{}, err
	}

	info, err := os.Stat(s.uploadFile(uploadID))
	if err != nil {
		if os.IsNotExist(err) {
			return domain.UploadState{}, domain.NewNotFoundError("upload", uploadID)
		}
		return domain.UploadState{}, err
	}

	state.Size = info.Size()
	state.UpdatedAt = info.ModTime().UTC()
	state.Location = s.uploadFile(uploadID)
	return state, nil
}

func (s *Store) PutUploadChunk(ctx context.Context, uploadID string, content io.Reader) (domain.UploadState, error) {
	file, err := os.OpenFile(s.uploadFile(uploadID), os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		if os.IsNotExist(err) {
			return domain.UploadState{}, domain.NewNotFoundError("upload", uploadID)
		}
		return domain.UploadState{}, err
	}
	defer file.Close()

	if _, err := io.Copy(file, content); err != nil {
		return domain.UploadState{}, err
	}

	state, err := s.GetUpload(ctx, uploadID)
	if err != nil {
		return domain.UploadState{}, err
	}

	if err := s.writeUploadState(state); err != nil {
		return domain.UploadState{}, err
	}

	return state, nil
}

func (s *Store) CommitUpload(_ context.Context, uploadID string, expected domain.Digest) (domain.Descriptor, error) {
	if err := expected.Validate(); err != nil {
		return domain.Descriptor{}, err
	}

	uploadPath := s.uploadFile(uploadID)
	file, err := os.Open(uploadPath)
	if err != nil {
		if os.IsNotExist(err) {
			return domain.Descriptor{}, domain.NewNotFoundError("upload", uploadID)
		}
		return domain.Descriptor{}, err
	}
	defer file.Close()

	actual, size, err := domain.DigestFromReader(file)
	if err != nil {
		return domain.Descriptor{}, err
	}

	if actual != expected {
		return domain.Descriptor{}, domain.NewDigestMismatchError(expected, actual)
	}

	finalPath := s.blobPath(actual)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return domain.Descriptor{}, err
	}

	if err := os.Rename(uploadPath, finalPath); err != nil {
		if !os.IsExist(err) {
			if _, statErr := os.Stat(finalPath); statErr != nil {
				return domain.Descriptor{}, err
			}
		}
	}

	if err := os.RemoveAll(s.uploadDir(uploadID)); err != nil {
		return domain.Descriptor{}, err
	}

	return domain.Descriptor{Digest: actual, Size: size}, nil
}

func (s *Store) CancelUpload(_ context.Context, uploadID string) error {
	if err := os.RemoveAll(s.uploadDir(uploadID)); err != nil {
		return err
	}

	return nil
}

func (s *Store) BlobExists(_ context.Context, digest domain.Digest) (bool, error) {
	if err := digest.Validate(); err != nil {
		return false, err
	}

	_, err := os.Stat(s.blobPath(digest))
	if err == nil {
		return true, nil
	}

	if os.IsNotExist(err) {
		return false, nil
	}

	return false, err
}

func (s *Store) OpenBlob(_ context.Context, digest domain.Digest) (io.ReadSeekCloser, domain.Descriptor, error) {
	if err := digest.Validate(); err != nil {
		return nil, domain.Descriptor{}, err
	}

	file, err := os.Open(s.blobPath(digest))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, domain.Descriptor{}, domain.NewNotFoundError("blob", digest.String())
		}
		return nil, domain.Descriptor{}, err
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, domain.Descriptor{}, err
	}

	return file, domain.Descriptor{Digest: digest, Size: info.Size()}, nil
}

func (s *Store) blobsRoot() string {
	return filepath.Join(s.rootDir, "blobs")
}

func (s *Store) uploadsRoot() string {
	return filepath.Join(s.rootDir, "uploads")
}

func (s *Store) uploadDir(uploadID string) string {
	return filepath.Join(s.uploadsRoot(), uploadID)
}

func (s *Store) uploadFile(uploadID string) string {
	return filepath.Join(s.uploadDir(uploadID), "data")
}

func (s *Store) uploadStateFile(uploadID string) string {
	return filepath.Join(s.uploadDir(uploadID), "state.json")
}

func (s *Store) blobPath(digest domain.Digest) string {
	return filepath.Join(s.blobsRoot(), digest.Algorithm(), digest.Encoded())
}

func newUploadID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}

	return hex.EncodeToString(buffer), nil
}

func (s *Store) writeUploadState(state domain.UploadState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}

	return os.WriteFile(s.uploadStateFile(state.ID), payload, 0o644)
}

func (s *Store) readUploadState(uploadID string) (domain.UploadState, error) {
	payload, err := os.ReadFile(s.uploadStateFile(uploadID))
	if err != nil {
		if os.IsNotExist(err) {
			return domain.UploadState{}, domain.NewNotFoundError("upload", uploadID)
		}
		return domain.UploadState{}, err
	}

	var state domain.UploadState
	if err := json.Unmarshal(payload, &state); err != nil {
		return domain.UploadState{}, err
	}

	return state, nil
}
