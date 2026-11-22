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
	"sync"
	"sync/atomic"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// DefaultMaxUploadSize is the per-upload byte cap New applies unless
// WithMaxUploadSize overrides it (5 GiB).
const DefaultMaxUploadSize int64 = 5 << 30

type Store struct {
	rootDir       string
	maxUploadSize int64
	// linkFile publishes an upload into the content store without replacing
	// an existing blob. It is os.Link; tests inject failures to exercise the
	// copy fallback.
	linkFile func(oldPath string, newPath string) error
	// uploads holds one *uploadEntry per upload that has been touched by this
	// process. The entry serializes every writer of that upload against
	// CommitUpload so no write descriptor can outlive the digest check.
	uploads sync.Map
}

// uploadEntry is the per-upload write lock plus a closed flag. closed flips
// to true, once and permanently, when the upload is committed, cancelled or
// reaped: a writer that sees it (before writing, or between chunks of a body
// it is copying) stops and reports the upload as gone.
type uploadEntry struct {
	mu     sync.Mutex
	closed atomic.Bool
}

// Option configures a Store.
type Option func(*Store)

// WithMaxUploadSize caps the total bytes one upload may accumulate across all
// of its PATCH/PUT chunks. Zero or negative disables the cap.
func WithMaxUploadSize(bytes int64) Option {
	return func(s *Store) { s.maxUploadSize = bytes }
}

func New(rootDir string, options ...Option) (*Store, error) {
	store := &Store{rootDir: rootDir, maxUploadSize: DefaultMaxUploadSize, linkFile: os.Link}
	for _, option := range options {
		if option != nil {
			option(store)
		}
	}
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

func (s *Store) uploadEntry(uploadID string) *uploadEntry {
	entry, _ := s.uploads.LoadOrStore(uploadID, &uploadEntry{})
	return entry.(*uploadEntry)
}

// retire marks the entry closed and forgets it. Closing first means a writer
// that already holds the pointer can never write after retirement.
func (s *Store) retire(uploadID string, entry *uploadEntry) {
	entry.closed.Store(true)
	s.uploads.Delete(uploadID)
}

// abortableReader stops a body copy as soon as the upload is retired, so a
// cancelled or reaped upload does not keep consuming disk until the client
// finishes sending.
type abortableReader struct {
	reader   io.Reader
	entry    *uploadEntry
	uploadID string
}

func (r *abortableReader) Read(p []byte) (int, error) {
	if r.entry.closed.Load() {
		return 0, domain.NewNotFoundError("upload", r.uploadID)
	}
	return r.reader.Read(p)
}

func (s *Store) PutUploadChunk(ctx context.Context, uploadID string, content io.Reader) (domain.UploadState, error) {
	entry := s.uploadEntry(uploadID)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.closed.Load() {
		return domain.UploadState{}, domain.NewNotFoundError("upload", uploadID)
	}

	file, err := os.OpenFile(s.uploadFile(uploadID), os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		if os.IsNotExist(err) {
			s.uploads.CompareAndDelete(uploadID, entry)
			return domain.UploadState{}, domain.NewNotFoundError("upload", uploadID)
		}
		return domain.UploadState{}, err
	}
	// The descriptor is closed before PutUploadChunk returns, and the entry
	// lock is held until then: CommitUpload can never observe it open.
	defer file.Close()

	var source io.Reader = &abortableReader{reader: content, entry: entry, uploadID: uploadID}
	var before int64
	var remaining int64
	capped := s.maxUploadSize > 0
	if capped {
		info, err := file.Stat()
		if err != nil {
			return domain.UploadState{}, err
		}
		before = info.Size()
		remaining = s.maxUploadSize - before
		if remaining < 0 {
			remaining = 0
		}
		// One byte past the remaining budget is enough to detect overflow
		// without ever accepting more than the cap.
		source = io.LimitReader(source, remaining+1)
	}

	written, err := io.Copy(file, source)
	if capped && written > remaining {
		// Roll the whole chunk back so the upload stays at its last good size.
		if truncErr := file.Truncate(before); truncErr != nil {
			return domain.UploadState{}, truncErr
		}
		return domain.UploadState{}, domain.NewPayloadTooLargeError(fmt.Sprintf("upload exceeds the maximum size of %d bytes", s.maxUploadSize))
	}
	if err != nil {
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

// CommitUpload verifies the upload's digest and publishes it as an immutable
// blob. It refuses (conflict) while a write to the same upload is in flight,
// holds the upload's lock for the whole operation so no writer can reopen the
// file, makes the file read-only before hashing it, and links it into the
// content-addressed tree without ever replacing an existing blob.
func (s *Store) CommitUpload(_ context.Context, uploadID string, expected domain.Digest) (domain.Descriptor, error) {
	if err := expected.Validate(); err != nil {
		return domain.Descriptor{}, err
	}

	entry := s.uploadEntry(uploadID)
	if !entry.mu.TryLock() {
		return domain.Descriptor{}, domain.NewConflictError("upload has a write in progress; retry once it completes")
	}
	defer entry.mu.Unlock()
	if entry.closed.Load() {
		return domain.Descriptor{}, domain.NewNotFoundError("upload", uploadID)
	}

	uploadPath := s.uploadFile(uploadID)
	// No descriptor opened for writing exists past this point (writers are
	// serialized by the lock above); dropping the write bits also makes any
	// later open for writing fail.
	if err := os.Chmod(uploadPath, 0o444); err != nil {
		if os.IsNotExist(err) {
			s.retire(uploadID, entry)
			return domain.Descriptor{}, domain.NewNotFoundError("upload", uploadID)
		}
		return domain.Descriptor{}, err
	}

	file, err := os.Open(uploadPath)
	if err != nil {
		return domain.Descriptor{}, err
	}
	actual, size, err := domain.DigestFromReader(file)
	_ = file.Close()
	if err != nil {
		return domain.Descriptor{}, err
	}

	if actual != expected {
		// The upload stays open for more data; restore append access.
		_ = os.Chmod(uploadPath, 0o644)
		return domain.Descriptor{}, domain.NewDigestMismatchError(expected, actual)
	}

	finalPath := s.blobPath(actual)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return domain.Descriptor{}, err
	}

	// Hard-link instead of rename: link never replaces an existing blob
	// (EEXIST), so a committed blob's inode is only ever created here. When
	// the filesystem cannot hard-link (EXDEV, EPERM, ENOTSUP, some network or
	// overlay mounts) fall back to a verified copy.
	if err := s.linkFile(uploadPath, finalPath); err != nil && !os.IsExist(err) {
		if err := s.copyIntoStore(uploadPath, finalPath, actual); err != nil {
			return domain.Descriptor{}, err
		}
	}

	s.retire(uploadID, entry)
	if err := os.RemoveAll(s.uploadDir(uploadID)); err != nil {
		return domain.Descriptor{}, err
	}

	return domain.Descriptor{Digest: actual, Size: size}, nil
}

// copyIntoStore is the link(2) fallback: copy the verified upload into an
// O_EXCL temp file next to the final path, fsync it, re-verify the digest of
// what actually reached the disk, make it read-only, and rename it into place
// unless a blob already occupies the final path (which is kept). The temp file
// is removed on every failure, so no partial file is left in the store.
func (s *Store) copyIntoStore(uploadPath string, finalPath string, expected domain.Digest) (err error) {
	tempName, err := newUploadID()
	if err != nil {
		return err
	}
	tempPath := filepath.Join(filepath.Dir(finalPath), ".tmp-"+tempName)
	temp, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = temp.Close()
			_ = os.Remove(tempPath)
		}
	}()

	source, err := os.Open(uploadPath)
	if err != nil {
		return err
	}
	_, err = io.Copy(temp, source)
	_ = source.Close()
	if err != nil {
		return err
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}

	check, err := os.Open(tempPath)
	if err != nil {
		return err
	}
	copied, _, err := domain.DigestFromReader(check)
	_ = check.Close()
	if err != nil {
		return err
	}
	if copied != expected {
		err = domain.NewDigestMismatchError(expected, copied)
		return err
	}
	if err = os.Chmod(tempPath, 0o444); err != nil {
		return err
	}

	if _, statErr := os.Lstat(finalPath); statErr == nil {
		// Someone published the same digest meanwhile; keep theirs.
		err = os.Remove(tempPath)
		return err
	}
	return os.Rename(tempPath, finalPath)
}

func (s *Store) CancelUpload(_ context.Context, uploadID string) error {
	// Retire first, without waiting for the lock: a writer in the middle of a
	// slow body stops at its next read instead of blocking the cancel.
	s.retire(uploadID, s.uploadEntry(uploadID))
	return os.RemoveAll(s.uploadDir(uploadID))
}

// ReapUpload removes an abandoned upload only if no write or commit is in
// flight and its data file was last modified before olderThan. It reports
// whether the upload was removed. The check and the removal happen under the
// upload's lock, so an upload that is being committed or has just received a
// chunk is never deleted.
func (s *Store) ReapUpload(_ context.Context, uploadID string, olderThan time.Time) (bool, error) {
	entry := s.uploadEntry(uploadID)
	if !entry.mu.TryLock() {
		return false, nil
	}
	defer entry.mu.Unlock()

	info, err := os.Stat(s.uploadFile(uploadID))
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err == nil && !info.ModTime().Before(olderThan) {
		return false, nil
	}

	s.retire(uploadID, entry)
	if err := os.RemoveAll(s.uploadDir(uploadID)); err != nil {
		return false, err
	}
	return true, nil
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

// ListBlobs walks s.blobsRoot() only -- uploads/ is a different tree under
// s.rootDir and is never traversed here (ports.BlobStore.ListBlobs' blocking
// doc comment: enumerating an in-flight upload would expose a half-written
// push to the garbage collector).
func (s *Store) ListBlobs(_ context.Context) ([]ports.BlobFileInfo, error) {
	root := s.blobsRoot()
	var blobs []ports.BlobFileInfo

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return blobs, nil
		}
		return nil, err
	}

	for _, algorithmEntry := range entries {
		if !algorithmEntry.IsDir() {
			continue
		}

		algorithmDir := filepath.Join(root, algorithmEntry.Name())
		digestEntries, err := os.ReadDir(algorithmDir)
		if err != nil {
			return nil, err
		}

		for _, digestEntry := range digestEntries {
			if digestEntry.IsDir() {
				continue
			}

			digest, err := domain.ParseDigest(algorithmEntry.Name() + ":" + digestEntry.Name())
			if err != nil {
				continue
			}

			info, err := digestEntry.Info()
			if err != nil {
				return nil, err
			}

			blobs = append(blobs, ports.BlobFileInfo{
				Digest:  digest,
				Size:    info.Size(),
				ModTime: info.ModTime(),
			})
		}
	}

	return blobs, nil
}

// DeleteBlob unlinks exactly one committed blob file (Phase 8 -- the only
// phase permitted to add this os.Remove call). The digest is validated via
// domain.Digest.Validate() BEFORE any path is built, exactly like every
// other blob method (BlobExists, OpenBlob) -- a traversal-shaped or
// otherwise malformed digest is rejected without ever touching the
// filesystem. The path is resolved exclusively through blobPath, which is
// scoped under s.blobsRoot(); DeleteBlob has no way to construct a path
// under uploads/ (a different tree entirely -- see ListBlobs' doc comment).
// Idempotent: an already-absent file is (false, nil), never an error.
func (s *Store) DeleteBlob(_ context.Context, digest domain.Digest) (bool, error) {
	if err := digest.Validate(); err != nil {
		return false, err
	}

	if err := os.Remove(s.blobPath(digest)); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
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

// validUploadDirName matches the IDs newUploadID generates; anything else
// under the uploads root is never touched.
func validUploadDirName(name string) bool {
	if len(name) != 32 {
		return false
	}
	for _, r := range name {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// ReapOrphanUploads removes upload directories under the uploads root that
// have no metadata row (a crash between creating the directory and saving the
// row) and whose newest file is older than olderThan. hasRow reports whether
// an upload ID still has a row; a directory with a row is never removed. At
// most limit directories are removed per call. Only direct children of the
// uploads root named like an upload ID are considered, nothing is followed
// through symlinks (a stray symlink is unlinked, never its target), and an
// upload this process is writing or committing is skipped.
func (s *Store) ReapOrphanUploads(ctx context.Context, olderThan time.Time, limit int, hasRow func(ctx context.Context, uploadID string) (bool, error)) (int, error) {
	entries, err := os.ReadDir(s.uploadsRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	reaped := 0
	for _, entry := range entries {
		if limit > 0 && reaped >= limit {
			break
		}
		if err := ctx.Err(); err != nil {
			return reaped, err
		}
		name := entry.Name()
		if !validUploadDirName(name) {
			continue
		}
		dir := s.uploadDir(name)
		info, err := os.Lstat(dir)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			// Not something this store creates: unlink the entry itself when
			// stale, never follow it.
			if info.ModTime().Before(olderThan) {
				if err := os.Remove(dir); err == nil {
					reaped++
				}
			}
			continue
		}

		newest := info.ModTime()
		for _, child := range []string{s.uploadFile(name), s.uploadStateFile(name)} {
			if childInfo, err := os.Lstat(child); err == nil && childInfo.ModTime().After(newest) {
				newest = childInfo.ModTime()
			}
		}
		if !newest.Before(olderThan) {
			continue
		}

		// An upload this process has touched is handled by ReapUpload.
		if _, known := s.uploads.Load(name); known {
			continue
		}
		fresh := &uploadEntry{}
		if _, loaded := s.uploads.LoadOrStore(name, fresh); loaded {
			continue
		}
		if !fresh.mu.TryLock() {
			continue
		}
		exists, err := hasRow(ctx, name)
		if err != nil || exists {
			fresh.mu.Unlock()
			s.uploads.CompareAndDelete(name, fresh)
			if err != nil {
				return reaped, err
			}
			continue
		}
		s.retire(name, fresh)
		fresh.mu.Unlock()
		if err := os.RemoveAll(dir); err != nil {
			return reaped, err
		}
		reaped++
	}

	return reaped, nil
}
