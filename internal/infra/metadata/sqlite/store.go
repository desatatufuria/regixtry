package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	domain "registry/internal/domain/registry"
)

type Store struct {
	db *sql.DB
}

func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	store := &Store{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) SaveUpload(ctx context.Context, tenant string, state domain.UploadState) error {
	if err := state.Validate(); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO uploads (id, tenant, repository, status, size, started_at, updated_at, location)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			repository = excluded.repository,
			status = excluded.status,
			size = excluded.size,
			started_at = excluded.started_at,
			updated_at = excluded.updated_at,
			location = excluded.location
	`, state.ID, tenant, state.Repository.String(), string(state.Status), state.Size, state.StartedAt.UTC().Format(time.RFC3339Nano), state.UpdatedAt.UTC().Format(time.RFC3339Nano), state.Location)
	return err
}

func (s *Store) GetUpload(ctx context.Context, tenant string, uploadID string) (domain.UploadState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT repository, status, size, started_at, updated_at, location
		FROM uploads
		WHERE tenant = ? AND id = ?
	`, tenant, uploadID)

	var repositoryName string
	var status string
	var size int64
	var startedAt string
	var updatedAt string
	var location string
	if err := row.Scan(&repositoryName, &status, &size, &startedAt, &updatedAt, &location); err != nil {
		if err == sql.ErrNoRows {
			return domain.UploadState{}, domain.NewNotFoundError("upload", uploadID)
		}
		return domain.UploadState{}, err
	}

	repository, err := domain.ParseRepositoryRef(repositoryName)
	if err != nil {
		return domain.UploadState{}, err
	}

	started, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return domain.UploadState{}, err
	}

	updated, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return domain.UploadState{}, err
	}

	return domain.UploadState{
		ID:         uploadID,
		Repository: repository,
		Status:     domain.UploadStatus(status),
		Size:       size,
		StartedAt:  started,
		UpdatedAt:  updated,
		Location:   location,
	}, nil
}

func (s *Store) ListUploads(ctx context.Context, tenant string, repository *domain.RepositoryRef) ([]domain.UploadState, error) {
	query := `
		SELECT id, repository, status, size, started_at, updated_at, location
		FROM uploads
		WHERE tenant = ?
	`
	args := []any{tenant}
	if repository != nil {
		query += ` AND repository = ?`
		args = append(args, repository.String())
	}
	query += ` ORDER BY started_at ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var uploads []domain.UploadState
	for rows.Next() {
		var uploadID string
		var repositoryName string
		var status string
		var size int64
		var startedAt string
		var updatedAt string
		var location string
		if err := rows.Scan(&uploadID, &repositoryName, &status, &size, &startedAt, &updatedAt, &location); err != nil {
			return nil, err
		}

		repositoryValue, err := domain.ParseRepositoryRef(repositoryName)
		if err != nil {
			return nil, err
		}

		started, err := time.Parse(time.RFC3339Nano, startedAt)
		if err != nil {
			return nil, err
		}

		updated, err := time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, err
		}

		uploads = append(uploads, domain.UploadState{
			ID:         uploadID,
			Repository: repositoryValue,
			Status:     domain.UploadStatus(status),
			Size:       size,
			StartedAt:  started,
			UpdatedAt:  updated,
			Location:   location,
		})
	}

	return uploads, rows.Err()
}

func (s *Store) DeleteUpload(ctx context.Context, tenant string, uploadID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM uploads WHERE tenant = ? AND id = ?`, tenant, uploadID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return domain.NewNotFoundError("upload", uploadID)
	}

	return nil
}

func (s *Store) PublishManifest(ctx context.Context, tenant string, repository domain.RepositoryRef, tag string, manifest domain.Manifest, blobs []domain.Descriptor) error {
	if err := repository.Validate(); err != nil {
		return err
	}

	if err := manifest.Validate(); err != nil {
		return err
	}

	for _, blob := range blobs {
		if err := blob.Validate(); err != nil {
			return err
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	repositoryID, err := ensureRepository(ctx, tx, tenant, repository)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO manifests (tenant, repository_id, digest, media_type, size, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant, repository_id, digest) DO UPDATE SET
			media_type = excluded.media_type,
			size = excluded.size,
			payload = excluded.payload
	`, tenant, repositoryID, manifest.Digest.String(), manifest.MediaType, manifest.Size, manifest.Payload, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}

	var manifestID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM manifests WHERE tenant = ? AND repository_id = ? AND digest = ?
	`, tenant, repositoryID, manifest.Digest.String()).Scan(&manifestID)
	if err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM manifest_blobs WHERE manifest_id = ?`, manifestID); err != nil {
		return err
	}

	for index, blob := range blobs {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO manifest_blobs (manifest_id, digest, media_type, size, position)
			VALUES (?, ?, ?, ?, ?)
		`, manifestID, blob.Digest.String(), blob.MediaType, blob.Size, index); err != nil {
			return err
		}
	}

	if tag != "" {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO tags (tenant, repository_id, name, manifest_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(tenant, repository_id, name) DO UPDATE SET
				manifest_id = excluded.manifest_id,
				updated_at = excluded.updated_at
		`, tenant, repositoryID, tag, manifestID, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
	}

	err = tx.Commit()
	return err
}

func (s *Store) ResolveManifest(ctx context.Context, tenant string, repository domain.RepositoryRef, reference string) (domain.Manifest, error) {
	if err := repository.Validate(); err != nil {
		return domain.Manifest{}, err
	}

	query := `
		SELECT m.media_type, m.payload
		FROM manifests m
		JOIN repositories r ON r.id = m.repository_id
		WHERE m.tenant = ? AND r.name = ? AND r.tenant = ?
	`
	args := []any{tenant, repository.String(), tenant}
	if _, err := domain.ParseDigest(reference); err == nil {
		query += ` AND m.digest = ?`
		args = append(args, reference)
	} else {
		query += ` AND m.id = (SELECT t.manifest_id FROM tags t WHERE t.tenant = ? AND t.repository_id = r.id AND t.name = ?)`
		args = append(args, tenant, reference)
	}

	var mediaType string
	var payload []byte
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&mediaType, &payload); err != nil {
		if err == sql.ErrNoRows {
			return domain.Manifest{}, domain.NewNotFoundError("manifest", reference)
		}
		return domain.Manifest{}, err
	}

	references, err := s.ListManifestBlobs(ctx, tenant, repository, domain.DigestFromBytes(payload))
	if err != nil {
		return domain.Manifest{}, err
	}

	manifest, err := domain.NewManifest(mediaType, payload, nil, references, nil, nil)
	if err != nil {
		return domain.Manifest{}, err
	}

	return manifest, nil
}

func (s *Store) Catalog(ctx context.Context, tenant string, limit int, after string) ([]domain.RepositoryRef, error) {
	query := `SELECT name FROM repositories WHERE tenant = ?`
	args := []any{tenant}
	if after != "" {
		query += ` AND name > ?`
		args = append(args, after)
	}
	query += ` ORDER BY name ASC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repositories []domain.RepositoryRef
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}

		repository, err := domain.ParseRepositoryRef(name)
		if err != nil {
			return nil, err
		}

		repositories = append(repositories, repository)
	}

	return repositories, rows.Err()
}

func (s *Store) ListTags(ctx context.Context, tenant string, repository domain.RepositoryRef, limit int, after string) ([]string, error) {
	if err := repository.Validate(); err != nil {
		return nil, err
	}

	query := `
		SELECT t.name
		FROM tags t
		JOIN repositories r ON r.id = t.repository_id
		WHERE t.tenant = ? AND r.tenant = ? AND r.name = ?
	`
	args := []any{tenant, tenant, repository.String()}
	if after != "" {
		query += ` AND t.name > ?`
		args = append(args, after)
	}
	query += ` ORDER BY t.name ASC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}

	return tags, rows.Err()
}

func (s *Store) ListManifestBlobs(ctx context.Context, tenant string, repository domain.RepositoryRef, manifestDigest domain.Digest) ([]domain.Descriptor, error) {
	if err := repository.Validate(); err != nil {
		return nil, err
	}

	if err := manifestDigest.Validate(); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT mb.media_type, mb.digest, mb.size
		FROM manifest_blobs mb
		JOIN manifests m ON m.id = mb.manifest_id
		JOIN repositories r ON r.id = m.repository_id
		WHERE m.tenant = ? AND r.tenant = ? AND r.name = ? AND m.digest = ?
		ORDER BY mb.position ASC
	`, tenant, tenant, repository.String(), manifestDigest.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	descriptors := make([]domain.Descriptor, 0)
	for rows.Next() {
		var mediaType string
		var digestValue string
		var size int64
		if err := rows.Scan(&mediaType, &digestValue, &size); err != nil {
			return nil, err
		}

		digest, err := domain.ParseDigest(digestValue)
		if err != nil {
			return nil, err
		}

		descriptors = append(descriptors, domain.Descriptor{MediaType: mediaType, Digest: digest, Size: size})
	}

	return descriptors, rows.Err()
}

func (s *Store) init() error {
	statements := []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS repositories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant TEXT NOT NULL,
			name TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(tenant, name)
		);`,
		`CREATE TABLE IF NOT EXISTS manifests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant TEXT NOT NULL,
			repository_id INTEGER NOT NULL,
			digest TEXT NOT NULL,
			media_type TEXT NOT NULL,
			size INTEGER NOT NULL,
			payload BLOB NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(tenant, repository_id, digest),
			FOREIGN KEY(repository_id) REFERENCES repositories(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant TEXT NOT NULL,
			repository_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			manifest_id INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(tenant, repository_id, name),
			FOREIGN KEY(repository_id) REFERENCES repositories(id) ON DELETE CASCADE,
			FOREIGN KEY(manifest_id) REFERENCES manifests(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS manifest_blobs (
			manifest_id INTEGER NOT NULL,
			digest TEXT NOT NULL,
			media_type TEXT NOT NULL,
			size INTEGER NOT NULL,
			position INTEGER NOT NULL,
			PRIMARY KEY(manifest_id, position),
			FOREIGN KEY(manifest_id) REFERENCES manifests(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS uploads (
			id TEXT PRIMARY KEY,
			tenant TEXT NOT NULL,
			repository TEXT NOT NULL,
			status TEXT NOT NULL,
			size INTEGER NOT NULL,
			started_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			location TEXT NOT NULL
		);`,
	}

	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}

	return nil
}

func ensureRepository(ctx context.Context, tx *sql.Tx, tenant string, repository domain.RepositoryRef) (int64, error) {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO repositories (tenant, name, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT(tenant, name) DO NOTHING
	`, tenant, repository.String(), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}

	var repositoryID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM repositories WHERE tenant = ? AND name = ?`, tenant, repository.String()).Scan(&repositoryID)
	return repositoryID, err
}

func normalizeReference(reference string) string {
	return strings.TrimSpace(reference)
}
