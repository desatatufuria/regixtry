package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

type Store struct {
	db *sql.DB
}

const sqliteBusyTimeoutMillis = 100

func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", sqliteDSN(path))
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

func sqliteDSN(path string) string {
	query := url.Values{}
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "busy_timeout("+strconv.Itoa(sqliteBusyTimeoutMillis)+")")

	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
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

// ListTagsWithCreatedAt is ListTags plus each tag's manifest created_at,
// joined from the manifests table (not the tags table's own created_at) so a
// retag of an existing digest still reports the manifest's original push
// time for every tag pointing at it (console-tags-table change).
func (s *Store) ListTagsWithCreatedAt(ctx context.Context, tenant string, repository domain.RepositoryRef, limit int, after string) ([]ports.TagSummary, error) {
	if err := repository.Validate(); err != nil {
		return nil, err
	}

	query := `
		SELECT t.name, m.created_at
		FROM tags t
		JOIN repositories r ON r.id = t.repository_id
		JOIN manifests m ON m.id = t.manifest_id
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

	var tags []ports.TagSummary
	for rows.Next() {
		var name string
		var createdAt string
		if err := rows.Scan(&name, &createdAt); err != nil {
			return nil, err
		}
		created, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, err
		}
		tags = append(tags, ports.TagSummary{Name: name, CreatedAt: created})
	}

	return tags, rows.Err()
}

// ListRepositoriesWithSummary is Catalog plus each repository's tag count
// and most recent manifest created_at across its tags (console-
// repositories-table change), computed with a single LEFT JOIN
// repositories -> tags -> manifests aggregate (COUNT/MAX GROUP BY
// repository) rather than one ListTagsWithCreatedAt call per repository --
// see the MetadataStore interface's own comment for why an aggregate query
// is the right choice here, unlike TagDetails' per-tag resolution. The LEFT
// JOINs (not INNER) keep a repository with zero tags in the result, with
// TagCount 0 and a zero LastPushed.
func (s *Store) ListRepositoriesWithSummary(ctx context.Context, tenant string, limit int, after string) ([]ports.RepositorySummary, error) {
	query := `
		SELECT r.name, COUNT(t.id) AS tag_count, MAX(m.created_at) AS last_pushed
		FROM repositories r
		LEFT JOIN tags t ON t.repository_id = r.id AND t.tenant = r.tenant
		LEFT JOIN manifests m ON m.id = t.manifest_id
		WHERE r.tenant = ?
	`
	args := []any{tenant}
	if after != "" {
		query += ` AND r.name > ?`
		args = append(args, after)
	}
	query += ` GROUP BY r.id, r.name ORDER BY r.name ASC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []ports.RepositorySummary
	for rows.Next() {
		var name string
		var tagCount int
		var lastPushed sql.NullString
		if err := rows.Scan(&name, &tagCount, &lastPushed); err != nil {
			return nil, err
		}

		summary := ports.RepositorySummary{Name: name, TagCount: tagCount}
		if lastPushed.Valid {
			pushed, err := time.Parse(time.RFC3339Nano, lastPushed.String)
			if err != nil {
				return nil, err
			}
			summary.LastPushed = pushed
		}
		summaries = append(summaries, summary)
	}

	return summaries, rows.Err()
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

func (s *Store) GetScanSettings(ctx context.Context, tenant string, feature string) (ports.ScanSettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT enabled, schedule_enabled, interval, timeout, cache_dir, binary_path, service_url, registry_reachable_url, auth_token, tls_ca_cert_path, tls_insecure_skip_verify, max_concurrency, updated_at
		FROM scan_settings
		WHERE tenant = ? AND feature = ?
	`, tenant, feature)
	var (
		enabled               bool
		scheduleEnabled       bool
		intervalRaw           string
		timeoutRaw            string
		cacheDir              string
		binaryPath            string
		serviceURL            string
		registryReachableURL  string
		authToken             string
		tlsCACertPath         string
		tlsInsecureSkipVerify bool
		maxConcurrency        int
		updatedAtRaw          string
	)
	if err := row.Scan(&enabled, &scheduleEnabled, &intervalRaw, &timeoutRaw, &cacheDir, &binaryPath, &serviceURL, &registryReachableURL, &authToken, &tlsCACertPath, &tlsInsecureSkipVerify, &maxConcurrency, &updatedAtRaw); err != nil {
		if err == sql.ErrNoRows {
			return ports.ScanSettings{}, domain.NewNotFoundError("scan_settings", tenant)
		}
		return ports.ScanSettings{}, err
	}
	interval, err := time.ParseDuration(intervalRaw)
	if err != nil {
		return ports.ScanSettings{}, err
	}
	timeout, err := time.ParseDuration(timeoutRaw)
	if err != nil {
		return ports.ScanSettings{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return ports.ScanSettings{}, err
	}
	return ports.ScanSettings{Enabled: enabled, ScheduleEnabled: scheduleEnabled, Interval: interval, Timeout: timeout, ServiceURL: serviceURL, RegistryReachableURL: registryReachableURL, AuthToken: authToken, TLSCACertPath: tlsCACertPath, TLSInsecureSkipVerify: tlsInsecureSkipVerify, LegacyCacheDir: cacheDir, LegacyBinaryPath: binaryPath, MaxConcurrency: maxConcurrency, UpdatedAt: updatedAt}, nil
}

func (s *Store) UpsertScanSettings(ctx context.Context, tenant string, feature string, settings ports.ScanSettings) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO scan_settings (tenant, feature, enabled, schedule_enabled, interval, timeout, cache_dir, binary_path, service_url, registry_reachable_url, auth_token, tls_ca_cert_path, tls_insecure_skip_verify, max_concurrency, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant, feature) DO UPDATE SET
			enabled = excluded.enabled,
			schedule_enabled = excluded.schedule_enabled,
			interval = excluded.interval,
			timeout = excluded.timeout,
			cache_dir = excluded.cache_dir,
			binary_path = excluded.binary_path,
			service_url = excluded.service_url,
			registry_reachable_url = excluded.registry_reachable_url,
			auth_token = excluded.auth_token,
			tls_ca_cert_path = excluded.tls_ca_cert_path,
			tls_insecure_skip_verify = excluded.tls_insecure_skip_verify,
			max_concurrency = excluded.max_concurrency,
			updated_at = excluded.updated_at
	`, tenant, feature, settings.Enabled, settings.ScheduleEnabled, settings.Interval.String(), settings.Timeout.String(), settings.LegacyCacheDir, settings.LegacyBinaryPath, settings.ServiceURL, settings.RegistryReachableURL, settings.AuthToken, settings.TLSCACertPath, settings.TLSInsecureSkipVerify, settings.MaxConcurrency, settings.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetScanPolicySettings(ctx context.Context, tenant string) (ports.ScanPolicySettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT enabled, severity_threshold, updated_at
		FROM scan_policy_settings
		WHERE tenant = ?
	`, tenant)
	var (
		enabled           bool
		severityThreshold string
		updatedAtRaw      string
	)
	if err := row.Scan(&enabled, &severityThreshold, &updatedAtRaw); err != nil {
		if err == sql.ErrNoRows {
			return ports.ScanPolicySettings{}, domain.NewNotFoundError("scan_policy_settings", tenant)
		}
		return ports.ScanPolicySettings{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return ports.ScanPolicySettings{}, err
	}
	return ports.ScanPolicySettings{Enabled: enabled, SeverityThreshold: severityThreshold, UpdatedAt: updatedAt}, nil
}

func (s *Store) UpsertScanPolicySettings(ctx context.Context, tenant string, settings ports.ScanPolicySettings) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO scan_policy_settings (tenant, enabled, severity_threshold, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(tenant) DO UPDATE SET
			enabled = excluded.enabled,
			severity_threshold = excluded.severity_threshold,
			updated_at = excluded.updated_at
	`, tenant, settings.Enabled, settings.SeverityThreshold, settings.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// GetSigningPolicySettings mirrors GetScanPolicySettings' row-absence
// behavior: a missing row is a typed domain.ErrorCodeNotFound, never a
// silent code-level default (design.md Decision 4). The code-level default
// applied on NotFound lives one layer up, at the service, and — unlike the
// scan gate — resolves to disabled, not enabled.
func (s *Store) GetSigningPolicySettings(ctx context.Context, tenant string) (ports.SigningPolicySettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT enabled, trusted_public_keys, updated_at
		FROM signing_policy_settings
		WHERE tenant = ?
	`, tenant)
	var (
		enabled         bool
		trustedKeysJSON string
		updatedAtRaw    string
	)
	if err := row.Scan(&enabled, &trustedKeysJSON, &updatedAtRaw); err != nil {
		if err == sql.ErrNoRows {
			return ports.SigningPolicySettings{}, domain.NewNotFoundError("signing_policy_settings", tenant)
		}
		return ports.SigningPolicySettings{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return ports.SigningPolicySettings{}, err
	}
	var trustedKeys []string
	if strings.TrimSpace(trustedKeysJSON) != "" {
		if err := json.Unmarshal([]byte(trustedKeysJSON), &trustedKeys); err != nil {
			return ports.SigningPolicySettings{}, err
		}
	}
	return ports.SigningPolicySettings{Enabled: enabled, TrustedPublicKeys: trustedKeys, UpdatedAt: updatedAt}, nil
}

// UpsertSigningPolicySettings mirrors UpsertScanPolicySettings's
// insert-or-replace shape, adding trusted_public_keys as the store's second
// JSON column (design.md Decision 4), the same bounded-blast-radius
// reasoning as repository_feature_overrides' payload column: no query ever
// filters, sorts, or joins on a key, every read is by the full (tenant) key.
func (s *Store) UpsertSigningPolicySettings(ctx context.Context, tenant string, settings ports.SigningPolicySettings) error {
	trustedKeysJSON, err := json.Marshal(settings.TrustedPublicKeys)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO signing_policy_settings (tenant, enabled, trusted_public_keys, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(tenant) DO UPDATE SET
			enabled = excluded.enabled,
			trusted_public_keys = excluded.trusted_public_keys,
			updated_at = excluded.updated_at
	`, tenant, settings.Enabled, string(trustedKeysJSON), settings.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetRepositoryFeatureOverride(ctx context.Context, tenant string, repository string, feature string) ([]byte, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT payload
		FROM repository_feature_overrides
		WHERE tenant = ? AND repository = ? AND feature_name = ?
	`, tenant, repository, feature)

	var payload string
	if err := row.Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.NewNotFoundError("repository_feature_override", repository+"/"+feature)
		}
		return nil, err
	}

	return []byte(payload), nil
}

func (s *Store) ListRepositoryFeatureOverrides(ctx context.Context, tenant string, feature string) ([]ports.RepositoryFeatureOverride, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT repository, payload, updated_at
		FROM repository_feature_overrides
		WHERE tenant = ? AND feature_name = ?
		ORDER BY repository ASC
	`, tenant, feature)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	overrides := make([]ports.RepositoryFeatureOverride, 0)
	for rows.Next() {
		var repository string
		var payload string
		var updatedAtRaw string
		if err := rows.Scan(&repository, &payload, &updatedAtRaw); err != nil {
			return nil, err
		}

		updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
		if err != nil {
			return nil, err
		}

		overrides = append(overrides, ports.RepositoryFeatureOverride{
			Repository: repository,
			Feature:    feature,
			Payload:    json.RawMessage(payload),
			UpdatedAt:  updatedAt,
		})
	}

	return overrides, rows.Err()
}

func (s *Store) UpsertRepositoryFeatureOverride(ctx context.Context, tenant string, repository string, feature string, payload []byte) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO repository_feature_overrides (tenant, repository, feature_name, payload, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(tenant, repository, feature_name) DO UPDATE SET
			payload = excluded.payload,
			updated_at = excluded.updated_at
	`, tenant, repository, feature, string(payload), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) DeleteRepositoryFeatureOverride(ctx context.Context, tenant string, repository string, feature string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM repository_feature_overrides WHERE tenant = ? AND repository = ? AND feature_name = ?
	`, tenant, repository, feature)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return domain.NewNotFoundError("repository_feature_override", repository+"/"+feature)
	}

	return nil
}

func (s *Store) GetFeatureRuntimeState(ctx context.Context, tenant string, feature string) (ports.FeatureRuntimeState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT status, active_version, previous_version, active_binary_path, cache_dir, receipt_path, migration_hint, last_verified_at, last_health_check_at, last_db_updated_at, last_error, updated_at
		FROM feature_runtime_state
		WHERE tenant = ? AND feature = ?
	`, tenant, feature)
	state, err := scanFeatureRuntimeStateRow(row)
	if err == nil {
		return state, nil
	}
	if err != sql.ErrNoRows {
		return ports.FeatureRuntimeState{}, err
	}
	settings, settingsErr := s.GetScanSettings(ctx, tenant, feature)
	if settingsErr != nil {
		if domain.IsCode(settingsErr, domain.ErrorCodeNotFound) {
			return ports.FeatureRuntimeState{}, domain.NewNotFoundError("feature_runtime_state", tenant)
		}
		return ports.FeatureRuntimeState{}, settingsErr
	}
	if legacyState, ok := deriveLegacyFeatureRuntimeState(settings); ok {
		return legacyState, nil
	}
	return ports.FeatureRuntimeState{}, domain.NewNotFoundError("feature_runtime_state", tenant)
}

func (s *Store) UpsertFeatureRuntimeState(ctx context.Context, tenant string, feature string, state ports.FeatureRuntimeState) error {
	var (
		lastVerifiedAt any
		lastHealthAt   any
		lastDBUpdated  any
	)
	if state.LastVerifiedAt != nil {
		lastVerifiedAt = state.LastVerifiedAt.UTC().Format(time.RFC3339Nano)
	}
	if state.LastHealthCheckAt != nil {
		lastHealthAt = state.LastHealthCheckAt.UTC().Format(time.RFC3339Nano)
	}
	if state.LastDBUpdatedAt != nil {
		lastDBUpdated = state.LastDBUpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO feature_runtime_state (tenant, feature, status, active_version, previous_version, active_binary_path, cache_dir, receipt_path, migration_hint, last_verified_at, last_health_check_at, last_db_updated_at, last_error, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant, feature) DO UPDATE SET
			status = excluded.status,
			active_version = excluded.active_version,
			previous_version = excluded.previous_version,
			active_binary_path = excluded.active_binary_path,
			cache_dir = excluded.cache_dir,
			receipt_path = excluded.receipt_path,
			migration_hint = excluded.migration_hint,
			last_verified_at = excluded.last_verified_at,
			last_health_check_at = excluded.last_health_check_at,
			last_db_updated_at = excluded.last_db_updated_at,
			last_error = excluded.last_error,
			updated_at = excluded.updated_at
	`, tenant, feature, string(state.Status), state.ActiveVersion, state.PreviousVersion, state.ActiveBinaryPath, state.CacheDir, state.ReceiptPath, state.MigrationHint, lastVerifiedAt, lastHealthAt, lastDBUpdated, state.LastError, state.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetActiveScanRunByDigest(ctx context.Context, tenant string, repository string, digest string) (ports.ScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, repository, requested_ref, digest, status, trigger, started_at, finished_at, created_at, updated_at, critical, high, medium, low, trivy_version, db_updated_at, error
		FROM scan_runs
		WHERE tenant = ? AND repository = ? AND digest = ? AND status IN (?, ?)
		ORDER BY created_at DESC
		LIMIT 1
	`, tenant, repository, digest, ports.ScanRunStatusQueued, ports.ScanRunStatusRunning)
	return scanRunRow(row)
}

// GetLatestScanRunByDigest returns the newest scan run for a digest
// regardless of status (design.md Decision 2: GetActiveScanRunByDigest
// minus the `status IN (?, ?)` predicate). Not-found is always the typed
// domain.ErrorCodeNotFound error via scanRunRow, never a zero-value
// ports.ScanRun that could be misread as a clean completed scan.
func (s *Store) GetLatestScanRunByDigest(ctx context.Context, tenant string, repository string, digest string) (ports.ScanRun, error) {
	// rowid DESC is a secondary sort key so a created_at tie (two runs
	// sharing an identical RFC3339Nano string) still resolves to the
	// actually-latest insert deterministically: rowid strictly increases
	// with every INSERT regardless of timestamp collisions, unlike id
	// (a random UUID) or created_at alone (live-DB confirmed unstable —
	// design.md's Open Questions).
	row := s.db.QueryRowContext(ctx, `
		SELECT id, repository, requested_ref, digest, status, trigger, started_at, finished_at, created_at, updated_at, critical, high, medium, low, trivy_version, db_updated_at, error
		FROM scan_runs
		WHERE tenant = ? AND repository = ? AND digest = ?
		ORDER BY created_at DESC, rowid DESC
		LIMIT 1
	`, tenant, repository, digest)
	return scanRunRow(row)
}

func (s *Store) GetScanRun(ctx context.Context, tenant string, runID string) (ports.ScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, repository, requested_ref, digest, status, trigger, started_at, finished_at, created_at, updated_at, critical, high, medium, low, trivy_version, db_updated_at, error
		FROM scan_runs
		WHERE tenant = ? AND id = ?
	`, tenant, runID)
	return scanRunRow(row)
}

func (s *Store) GetScanRunDetail(ctx context.Context, tenant string, runID string) (ports.ScanRunDetail, error) {
	run, err := s.GetScanRun(ctx, tenant, runID)
	if err != nil {
		return ports.ScanRunDetail{}, err
	}
	findings, err := s.listScanRunFindings(ctx, runID)
	if err != nil {
		return ports.ScanRunDetail{}, err
	}
	freshness, err := s.getScanRunDBFreshness(ctx, runID)
	if err != nil {
		return ports.ScanRunDetail{}, err
	}
	run.HasFixable = scanRunFindingsHaveFixable(findings)
	return ports.ScanRunDetail{Run: run, Findings: findings, DBFreshness: freshness}, nil
}

func (s *Store) UpsertScanRun(ctx context.Context, tenant string, run ports.ScanRun) error {
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = run.CreatedAt
	}
	var startedAt any
	if run.StartedAt != nil {
		startedAt = run.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	var finishedAt any
	if run.FinishedAt != nil {
		finishedAt = run.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	var dbUpdatedAt any
	if run.DBUpdatedAt != nil {
		dbUpdatedAt = run.DBUpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO scan_runs (id, tenant, repository, requested_ref, digest, status, trigger, started_at, finished_at, created_at, updated_at, critical, high, medium, low, trivy_version, db_updated_at, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			started_at = excluded.started_at,
			finished_at = excluded.finished_at,
			updated_at = excluded.updated_at,
			critical = excluded.critical,
			high = excluded.high,
			medium = excluded.medium,
			low = excluded.low,
			trivy_version = excluded.trivy_version,
			db_updated_at = excluded.db_updated_at,
			error = excluded.error
	`, run.ID, tenant, run.Repository, run.RequestedRef, run.Digest, run.Status, run.Trigger, startedAt, finishedAt, run.CreatedAt.UTC().Format(time.RFC3339Nano), run.UpdatedAt.UTC().Format(time.RFC3339Nano), run.Critical, run.High, run.Medium, run.Low, run.TrivyVersion, dbUpdatedAt, run.Error)
	return err
}

func (s *Store) UpsertScanRunDetail(ctx context.Context, tenant string, detail ports.ScanRunDetail) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = upsertScanRunTx(ctx, tx, tenant, detail.Run); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM scan_run_findings WHERE run_id = ?`, detail.Run.ID); err != nil {
		return err
	}
	for index, finding := range detail.Findings {
		var publishedAt any
		if finding.PublishedAt != nil {
			publishedAt = finding.PublishedAt.UTC().Format(time.RFC3339Nano)
		}
		var modifiedAt any
		if finding.ModifiedAt != nil {
			modifiedAt = finding.ModifiedAt.UTC().Format(time.RFC3339Nano)
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO scan_run_findings (run_id, position, target, class, type, severity, vulnerability_id, package_name, installed_version, fixed_version, title, primary_url, fixable, status, data_source, data_source_url, published_at, modified_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, detail.Run.ID, index, finding.Target, finding.Class, finding.Type, finding.Severity, finding.VulnerabilityID, finding.PackageName, finding.InstalledVersion, finding.FixedVersion, finding.Title, finding.PrimaryURL, finding.Fixable, finding.Status, finding.DataSource, finding.DataSourceURL, publishedAt, modifiedAt); err != nil {
			return err
		}
	}
	if hasDBFreshness(detail.DBFreshness) {
		var reportCreatedAt any
		if detail.DBFreshness.ReportCreatedAt != nil {
			reportCreatedAt = detail.DBFreshness.ReportCreatedAt.UTC().Format(time.RFC3339Nano)
		}
		var dbUpdatedAt any
		if detail.DBFreshness.DBUpdatedAt != nil {
			dbUpdatedAt = detail.DBFreshness.DBUpdatedAt.UTC().Format(time.RFC3339Nano)
		}
		var dbDownloadedAt any
		if detail.DBFreshness.DBDownloadedAt != nil {
			dbDownloadedAt = detail.DBFreshness.DBDownloadedAt.UTC().Format(time.RFC3339Nano)
		}
		var dbNextUpdateAt any
		if detail.DBFreshness.DBNextUpdateAt != nil {
			dbNextUpdateAt = detail.DBFreshness.DBNextUpdateAt.UTC().Format(time.RFC3339Nano)
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO scan_run_db_freshness (run_id, report_schema_version, report_created_at, trivy_version, db_version, db_updated_at, db_downloaded_at, db_next_update_at, freshness_state)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(run_id) DO UPDATE SET
				report_schema_version = excluded.report_schema_version,
				report_created_at = excluded.report_created_at,
				trivy_version = excluded.trivy_version,
				db_version = excluded.db_version,
				db_updated_at = excluded.db_updated_at,
				db_downloaded_at = excluded.db_downloaded_at,
				db_next_update_at = excluded.db_next_update_at,
				freshness_state = excluded.freshness_state
		`, detail.Run.ID, detail.DBFreshness.ReportSchemaVersion, reportCreatedAt, detail.DBFreshness.TrivyVersion, detail.DBFreshness.DBVersion, dbUpdatedAt, dbDownloadedAt, dbNextUpdateAt, detail.DBFreshness.FreshnessState); err != nil {
			return err
		}
	} else if _, err = tx.ExecContext(ctx, `DELETE FROM scan_run_db_freshness WHERE run_id = ?`, detail.Run.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListScanRuns(ctx context.Context, tenant string, repository string, limit int) ([]ports.ScanRun, error) {
	query := `
		SELECT id, repository, requested_ref, digest, status, trigger, started_at, finished_at, created_at, updated_at, critical, high, medium, low, trivy_version, db_updated_at, error
		FROM scan_runs
		WHERE tenant = ?
	`
	args := []any{tenant}
	if strings.TrimSpace(repository) != "" {
		query += ` AND repository = ?`
		args = append(args, repository)
	}
	query += ` ORDER BY CASE WHEN critical > 0 THEN 4 WHEN high > 0 THEN 3 WHEN medium > 0 THEN 2 WHEN low > 0 THEN 1 ELSE 0 END DESC,
		EXISTS(SELECT 1 FROM scan_run_findings findings WHERE findings.run_id = scan_runs.id AND findings.fixable = 1) DESC,
		created_at DESC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]ports.ScanRun, 0)
	for rows.Next() {
		run, err := scanRunRows(rows)
		if err != nil {
			return nil, err
		}
		run.HasFixable, err = s.scanRunHasFixable(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Store) GetActiveSecretScanRunByDigest(ctx context.Context, tenant string, repository string, digest string) (ports.SecretScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, repository, digest, status, trigger, started_at, finished_at, created_at, updated_at, gitleaks_version, error
		FROM secret_scan_runs
		WHERE tenant = ? AND repository = ? AND digest = ? AND status IN (?, ?)
		ORDER BY created_at DESC
		LIMIT 1
	`, tenant, repository, digest, ports.SecretScanRunStatusQueued, ports.SecretScanRunStatusRunning)
	return secretScanRunRow(row)
}

func (s *Store) GetSecretScanRun(ctx context.Context, tenant string, runID string) (ports.SecretScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, repository, digest, status, trigger, started_at, finished_at, created_at, updated_at, gitleaks_version, error
		FROM secret_scan_runs
		WHERE tenant = ? AND id = ?
	`, tenant, runID)
	return secretScanRunRow(row)
}

func (s *Store) GetSecretScanRunDetail(ctx context.Context, tenant string, runID string) (ports.SecretScanRunDetail, error) {
	run, err := s.GetSecretScanRun(ctx, tenant, runID)
	if err != nil {
		return ports.SecretScanRunDetail{}, err
	}
	findings, err := s.listSecretScanFindings(ctx, runID)
	if err != nil {
		return ports.SecretScanRunDetail{}, err
	}
	run.FindingCount = len(findings)
	return ports.SecretScanRunDetail{Run: run, Findings: findings}, nil
}

func (s *Store) UpsertSecretScanRun(ctx context.Context, tenant string, run ports.SecretScanRun) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = upsertSecretScanRunTx(ctx, tx, tenant, run); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpsertSecretScanRunDetail(ctx context.Context, tenant string, detail ports.SecretScanRunDetail) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = upsertSecretScanRunTx(ctx, tx, tenant, detail.Run); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM secret_scan_findings WHERE run_id = ?`, detail.Run.ID); err != nil {
		return err
	}
	for index, finding := range detail.Findings {
		tagsJSON, marshalErr := json.Marshal(finding.Tags)
		if marshalErr != nil {
			err = marshalErr
			return err
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO secret_scan_findings (run_id, position, rule_id, description, blob_digest, path, start_line, end_line, tags)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, detail.Run.ID, index, finding.RuleID, finding.Description, finding.BlobDigest, finding.Path, finding.StartLine, finding.EndLine, string(tagsJSON)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListSecretScanRuns(ctx context.Context, tenant string, repository string, limit int) ([]ports.SecretScanRun, error) {
	query := `
		SELECT id, repository, digest, status, trigger, started_at, finished_at, created_at, updated_at, gitleaks_version, error
		FROM secret_scan_runs
		WHERE tenant = ?
	`
	args := []any{tenant}
	if strings.TrimSpace(repository) != "" {
		query += ` AND repository = ?`
		args = append(args, repository)
	}
	query += ` ORDER BY created_at DESC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]ports.SecretScanRun, 0)
	for rows.Next() {
		run, err := secretScanRunRows(rows)
		if err != nil {
			return nil, err
		}
		findingCount, err := s.secretScanRunFindingCount(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		run.FindingCount = findingCount
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Store) TryAcquireScanSchedulerLease(ctx context.Context, tenant string, owner string, now time.Time, leaseTTL time.Duration) (bool, ports.ScanSchedulerState, error) {
	current, err := s.GetScanSchedulerState(ctx, tenant)
	if err != nil && !domain.IsCode(err, domain.ErrorCodeNotFound) {
		return false, ports.ScanSchedulerState{}, err
	}
	if err == nil && current.OwnerID != "" && current.OwnerID != owner && current.LeaseExpiresAt.After(now) {
		return false, current, nil
	}
	state := current
	state.OwnerID = owner
	state.LeaseExpiresAt = now.Add(leaseTTL)
	state.LastHeartbeatAt = now
	if state.BatchStartedAt == nil || current.LeaseExpiresAt.Before(now) {
		startedAt := now
		state.BatchStartedAt = &startedAt
	}
	if err := s.UpsertScanSchedulerState(ctx, tenant, state); err != nil {
		return false, ports.ScanSchedulerState{}, err
	}
	return true, state, nil
}

func (s *Store) HeartbeatScanScheduler(ctx context.Context, tenant string, owner string, now time.Time, leaseTTL time.Duration) error {
	state, err := s.GetScanSchedulerState(ctx, tenant)
	if err != nil {
		return err
	}
	state.OwnerID = owner
	state.LeaseExpiresAt = now.Add(leaseTTL)
	state.LastHeartbeatAt = now
	return s.UpsertScanSchedulerState(ctx, tenant, state)
}

func (s *Store) GetScanSchedulerState(ctx context.Context, tenant string) (ports.ScanSchedulerState, error) {
	row := s.db.QueryRowContext(ctx, `SELECT owner_id, lease_expires_at, last_heartbeat_at, batch_started_at FROM scan_scheduler_state WHERE tenant = ?`, tenant)
	var ownerID string
	var leaseExpiresAt string
	var lastHeartbeatAt string
	var batchStartedAt sql.NullString
	if err := row.Scan(&ownerID, &leaseExpiresAt, &lastHeartbeatAt, &batchStartedAt); err != nil {
		if err == sql.ErrNoRows {
			return ports.ScanSchedulerState{}, domain.NewNotFoundError("scan_scheduler_state", tenant)
		}
		return ports.ScanSchedulerState{}, err
	}
	leaseTime, err := time.Parse(time.RFC3339Nano, leaseExpiresAt)
	if err != nil {
		return ports.ScanSchedulerState{}, err
	}
	heartbeatTime, err := time.Parse(time.RFC3339Nano, lastHeartbeatAt)
	if err != nil {
		return ports.ScanSchedulerState{}, err
	}
	state := ports.ScanSchedulerState{OwnerID: ownerID, LeaseExpiresAt: leaseTime, LastHeartbeatAt: heartbeatTime}
	if batchStartedAt.Valid && strings.TrimSpace(batchStartedAt.String) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, batchStartedAt.String)
		if err != nil {
			return ports.ScanSchedulerState{}, err
		}
		state.BatchStartedAt = &parsed
	}
	return state, nil
}

func (s *Store) UpsertScanSchedulerState(ctx context.Context, tenant string, state ports.ScanSchedulerState) error {
	var batchStartedAt any
	if state.BatchStartedAt != nil {
		batchStartedAt = state.BatchStartedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO scan_scheduler_state (tenant, owner_id, lease_expires_at, last_heartbeat_at, batch_started_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(tenant) DO UPDATE SET
			owner_id = excluded.owner_id,
			lease_expires_at = excluded.lease_expires_at,
			last_heartbeat_at = excluded.last_heartbeat_at,
			batch_started_at = excluded.batch_started_at
	`, tenant, state.OwnerID, state.LeaseExpiresAt.UTC().Format(time.RFC3339Nano), state.LastHeartbeatAt.UTC().Format(time.RFC3339Nano), batchStartedAt)
	return err
}

func (s *Store) init() error {
	statements := []string{
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
		`CREATE TABLE IF NOT EXISTS scan_settings (
			tenant TEXT NOT NULL,
			feature TEXT NOT NULL DEFAULT 'trivy',
			enabled INTEGER NOT NULL,
			schedule_enabled INTEGER NOT NULL,
			interval TEXT NOT NULL,
			timeout TEXT NOT NULL,
			cache_dir TEXT NOT NULL,
			binary_path TEXT NOT NULL,
			service_url TEXT NOT NULL DEFAULT '',
			registry_reachable_url TEXT NOT NULL DEFAULT '',
			auth_token TEXT NOT NULL DEFAULT '',
			tls_ca_cert_path TEXT NOT NULL DEFAULT '',
			tls_insecure_skip_verify INTEGER NOT NULL DEFAULT 0,
			max_concurrency INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant, feature)
		);`,
		`ALTER TABLE scan_settings ADD COLUMN service_url TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE scan_settings ADD COLUMN registry_reachable_url TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE scan_settings ADD COLUMN auth_token TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE scan_settings ADD COLUMN tls_ca_cert_path TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE scan_settings ADD COLUMN tls_insecure_skip_verify INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE scan_settings ADD COLUMN feature TEXT NOT NULL DEFAULT 'trivy';`,
		`CREATE TABLE IF NOT EXISTS scan_runs (
			id TEXT PRIMARY KEY,
			tenant TEXT NOT NULL,
			repository TEXT NOT NULL,
			requested_ref TEXT NOT NULL,
			digest TEXT NOT NULL,
			status TEXT NOT NULL,
			trigger TEXT NOT NULL,
			started_at TEXT,
			finished_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			critical INTEGER NOT NULL,
			high INTEGER NOT NULL,
			medium INTEGER NOT NULL,
			low INTEGER NOT NULL,
			trivy_version TEXT NOT NULL,
			db_updated_at TEXT,
			error TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS scan_run_findings (
			run_id TEXT NOT NULL,
			position INTEGER NOT NULL,
			target TEXT NOT NULL DEFAULT '',
			class TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL DEFAULT '',
			severity TEXT NOT NULL DEFAULT '',
			vulnerability_id TEXT NOT NULL DEFAULT '',
			package_name TEXT NOT NULL DEFAULT '',
			installed_version TEXT NOT NULL DEFAULT '',
			fixed_version TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			primary_url TEXT NOT NULL DEFAULT '',
			fixable INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT '',
			data_source TEXT NOT NULL DEFAULT '',
			data_source_url TEXT NOT NULL DEFAULT '',
			published_at TEXT,
			modified_at TEXT,
			PRIMARY KEY(run_id, position),
			FOREIGN KEY(run_id) REFERENCES scan_runs(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS scan_run_db_freshness (
			run_id TEXT PRIMARY KEY,
			report_schema_version INTEGER NOT NULL DEFAULT 0,
			report_created_at TEXT,
			trivy_version TEXT NOT NULL DEFAULT '',
			db_version INTEGER NOT NULL DEFAULT 0,
			db_updated_at TEXT,
			db_downloaded_at TEXT,
			db_next_update_at TEXT,
			freshness_state TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(run_id) REFERENCES scan_runs(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS scan_scheduler_state (
			tenant TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			lease_expires_at TEXT NOT NULL,
			last_heartbeat_at TEXT NOT NULL,
			batch_started_at TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS trivy_runtime_state (
			tenant TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			active_version TEXT NOT NULL DEFAULT '',
			previous_version TEXT NOT NULL DEFAULT '',
			active_binary_path TEXT NOT NULL DEFAULT '',
			cache_dir TEXT NOT NULL DEFAULT '',
			receipt_path TEXT NOT NULL DEFAULT '',
			migration_hint TEXT NOT NULL DEFAULT '',
			last_verified_at TEXT,
			last_health_check_at TEXT,
			last_db_updated_at TEXT,
			last_error TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		);`,
		// trivy_runtime_state is superseded by feature_runtime_state (tenant, feature) below and is
		// intentionally left in place, unread: no production data and no migration shim in scope.
		`CREATE TABLE IF NOT EXISTS feature_runtime_state (
			tenant TEXT NOT NULL,
			feature TEXT NOT NULL,
			status TEXT NOT NULL,
			active_version TEXT NOT NULL DEFAULT '',
			previous_version TEXT NOT NULL DEFAULT '',
			active_binary_path TEXT NOT NULL DEFAULT '',
			cache_dir TEXT NOT NULL DEFAULT '',
			receipt_path TEXT NOT NULL DEFAULT '',
			migration_hint TEXT NOT NULL DEFAULT '',
			last_verified_at TEXT,
			last_health_check_at TEXT,
			last_db_updated_at TEXT,
			last_error TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant, feature)
		);`,
		`CREATE TABLE IF NOT EXISTS secret_scan_runs (
			id TEXT PRIMARY KEY,
			tenant TEXT NOT NULL,
			repository TEXT NOT NULL,
			digest TEXT NOT NULL,
			status TEXT NOT NULL,
			trigger TEXT NOT NULL,
			started_at TEXT,
			finished_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			gitleaks_version TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS scan_policy_settings (
			tenant TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			severity_threshold TEXT NOT NULL DEFAULT 'critical',
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant)
		);`,
		// enabled defaults to 0 here, deliberately inverted from
		// scan_policy_settings' DEFAULT 1 (design.md Decision 4): a
		// fail-closed content-trust gate must never default to ON with zero
		// trusted keys, or it would 403 every pull the moment this binary
		// boots on an existing deployment.
		`CREATE TABLE IF NOT EXISTS signing_policy_settings (
			tenant TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 0,
			trusted_public_keys TEXT NOT NULL DEFAULT '[]',
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant)
		);`,
		`CREATE TABLE IF NOT EXISTS secret_scan_findings (
			run_id TEXT NOT NULL,
			position INTEGER NOT NULL,
			rule_id TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			blob_digest TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL DEFAULT '',
			start_line INTEGER NOT NULL DEFAULT 0,
			end_line INTEGER NOT NULL DEFAULT 0,
			tags TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(run_id, position),
			FOREIGN KEY(run_id) REFERENCES secret_scan_runs(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS repository_feature_overrides (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant TEXT NOT NULL,
			repository TEXT NOT NULL,
			feature_name TEXT NOT NULL,
			payload TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(tenant, repository, feature_name)
		);`,
	}

	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			message := strings.ToLower(err.Error())
			if strings.Contains(message, "duplicate column name") {
				continue
			}
			return err
		}
	}

	return nil
}

func deriveLegacyFeatureRuntimeState(settings ports.ScanSettings) (ports.FeatureRuntimeState, bool) {
	legacyBinary := strings.TrimSpace(settings.LegacyBinaryPath)
	legacyCache := strings.TrimSpace(settings.LegacyCacheDir)
	legacyService := strings.TrimSpace(settings.ServiceURL)
	if legacyBinary == "" && legacyCache == "" && legacyService == "" {
		return ports.FeatureRuntimeState{}, false
	}
	hintParts := make([]string, 0, 3)
	if legacyBinary != "" {
		hintParts = append(hintParts, fmt.Sprintf("legacy binary_path %q requires managed reinstall and will never be executed", legacyBinary))
	}
	if legacyCache != "" {
		hintParts = append(hintParts, fmt.Sprintf("legacy cache_dir %q is migration evidence only", legacyCache))
	}
	if legacyService != "" {
		hintParts = append(hintParts, fmt.Sprintf("legacy service_url %q is superseded by the managed runtime", legacyService))
	}
	return ports.FeatureRuntimeState{
		Status:        ports.FeatureRuntimeStatusMigrationRequired,
		MigrationHint: strings.Join(hintParts, "; "),
		UpdatedAt:     settings.UpdatedAt,
	}, true
}

func scanFeatureRuntimeStateRow(row scanRunScanner) (ports.FeatureRuntimeState, error) {
	var (
		statusRaw        string
		activeVersion    string
		previousVersion  string
		activeBinaryPath string
		cacheDir         string
		receiptPath      string
		migrationHint    string
		lastVerifiedRaw  sql.NullString
		lastHealthRaw    sql.NullString
		lastDBUpdatedRaw sql.NullString
		lastError        string
		updatedAtRaw     string
	)
	if err := row.Scan(&statusRaw, &activeVersion, &previousVersion, &activeBinaryPath, &cacheDir, &receiptPath, &migrationHint, &lastVerifiedRaw, &lastHealthRaw, &lastDBUpdatedRaw, &lastError, &updatedAtRaw); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	state := ports.FeatureRuntimeState{
		Status:           ports.FeatureRuntimeStatus(statusRaw),
		ActiveVersion:    activeVersion,
		PreviousVersion:  previousVersion,
		ActiveBinaryPath: activeBinaryPath,
		CacheDir:         cacheDir,
		ReceiptPath:      receiptPath,
		MigrationHint:    migrationHint,
		LastError:        lastError,
		UpdatedAt:        updatedAt,
	}
	if state.LastVerifiedAt, err = parseOptionalTime(lastVerifiedRaw); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	if state.LastHealthCheckAt, err = parseOptionalTime(lastHealthRaw); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	if state.LastDBUpdatedAt, err = parseOptionalTime(lastDBUpdatedRaw); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	return state, nil
}

func parseOptionalTime(raw sql.NullString) (*time.Time, error) {
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
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

type scanRunScanner interface {
	Scan(dest ...any) error
}

func scanRunRow(row scanRunScanner) (ports.ScanRun, error) {
	run, err := scanRunFromScanner(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return ports.ScanRun{}, domain.NewNotFoundError("scan_run", "")
		}
		return ports.ScanRun{}, err
	}
	return run, nil
}

func scanRunRows(rows *sql.Rows) (ports.ScanRun, error) {
	return scanRunFromScanner(rows)
}

func listScanRunFindings(ctx context.Context, db queryer, runID string) ([]ports.ScanRunFinding, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT target, class, type, severity, vulnerability_id, package_name, installed_version, fixed_version, title, primary_url, fixable, status, data_source, data_source_url, published_at, modified_at
		FROM scan_run_findings
		WHERE run_id = ?
		ORDER BY CASE WHEN severity = 'CRITICAL' THEN 4 WHEN severity = 'HIGH' THEN 3 WHEN severity = 'MEDIUM' THEN 2 WHEN severity = 'LOW' THEN 1 ELSE 0 END DESC,
			fixable DESC,
			position ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	findings := make([]ports.ScanRunFinding, 0)
	for rows.Next() {
		finding, scanErr := scanRunFindingRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		findings = append(findings, finding)
	}
	return findings, rows.Err()
}

func (s *Store) listScanRunFindings(ctx context.Context, runID string) ([]ports.ScanRunFinding, error) {
	return listScanRunFindings(ctx, s.db, runID)
}

func (s *Store) getScanRunDBFreshness(ctx context.Context, runID string) (ports.ScanRunDBFreshness, error) {
	row := s.db.QueryRowContext(ctx, `SELECT report_schema_version, report_created_at, trivy_version, db_version, db_updated_at, db_downloaded_at, db_next_update_at, freshness_state FROM scan_run_db_freshness WHERE run_id = ?`, runID)
	var freshness ports.ScanRunDBFreshness
	var reportCreatedAt sql.NullString
	var dbUpdatedAt sql.NullString
	var dbDownloadedAt sql.NullString
	var dbNextUpdateAt sql.NullString
	if err := row.Scan(&freshness.ReportSchemaVersion, &reportCreatedAt, &freshness.TrivyVersion, &freshness.DBVersion, &dbUpdatedAt, &dbDownloadedAt, &dbNextUpdateAt, &freshness.FreshnessState); err != nil {
		if err == sql.ErrNoRows {
			return ports.ScanRunDBFreshness{FreshnessState: ports.ScanRunDBFreshnessStateUnknown}, nil
		}
		return ports.ScanRunDBFreshness{}, err
	}
	var err error
	if freshness.ReportCreatedAt, err = parseOptionalTime(reportCreatedAt); err != nil {
		return ports.ScanRunDBFreshness{}, err
	}
	if freshness.DBUpdatedAt, err = parseOptionalTime(dbUpdatedAt); err != nil {
		return ports.ScanRunDBFreshness{}, err
	}
	if freshness.DBDownloadedAt, err = parseOptionalTime(dbDownloadedAt); err != nil {
		return ports.ScanRunDBFreshness{}, err
	}
	if freshness.DBNextUpdateAt, err = parseOptionalTime(dbNextUpdateAt); err != nil {
		return ports.ScanRunDBFreshness{}, err
	}
	if strings.TrimSpace(freshness.FreshnessState) == "" {
		freshness.FreshnessState = ports.ScanRunDBFreshnessStateUnknown
	}
	return freshness, nil
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func scanRunFindingRow(scanner scanRunScanner) (ports.ScanRunFinding, error) {
	var finding ports.ScanRunFinding
	var publishedAt sql.NullString
	var modifiedAt sql.NullString
	if err := scanner.Scan(&finding.Target, &finding.Class, &finding.Type, &finding.Severity, &finding.VulnerabilityID, &finding.PackageName, &finding.InstalledVersion, &finding.FixedVersion, &finding.Title, &finding.PrimaryURL, &finding.Fixable, &finding.Status, &finding.DataSource, &finding.DataSourceURL, &publishedAt, &modifiedAt); err != nil {
		return ports.ScanRunFinding{}, err
	}
	var err error
	if finding.PublishedAt, err = parseOptionalTime(publishedAt); err != nil {
		return ports.ScanRunFinding{}, err
	}
	if finding.ModifiedAt, err = parseOptionalTime(modifiedAt); err != nil {
		return ports.ScanRunFinding{}, err
	}
	return finding, nil
}

func (s *Store) scanRunHasFixable(ctx context.Context, runID string) (bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM scan_run_findings WHERE run_id = ? AND fixable = 1)`, runID)
	var hasFixable bool
	if err := row.Scan(&hasFixable); err != nil {
		return false, err
	}
	return hasFixable, nil
}

func scanRunFindingsHaveFixable(findings []ports.ScanRunFinding) bool {
	for _, finding := range findings {
		if finding.Fixable {
			return true
		}
	}
	return false
}

func hasDBFreshness(freshness ports.ScanRunDBFreshness) bool {
	return freshness.ReportSchemaVersion > 0 || freshness.ReportCreatedAt != nil || strings.TrimSpace(freshness.TrivyVersion) != "" || freshness.DBVersion > 0 || freshness.DBUpdatedAt != nil || freshness.DBDownloadedAt != nil || freshness.DBNextUpdateAt != nil || strings.TrimSpace(freshness.FreshnessState) != ""
}

func upsertScanRunTx(ctx context.Context, tx *sql.Tx, tenant string, run ports.ScanRun) error {
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = run.CreatedAt
	}
	var startedAt any
	if run.StartedAt != nil {
		startedAt = run.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	var finishedAt any
	if run.FinishedAt != nil {
		finishedAt = run.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	var dbUpdatedAt any
	if run.DBUpdatedAt != nil {
		dbUpdatedAt = run.DBUpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO scan_runs (id, tenant, repository, requested_ref, digest, status, trigger, started_at, finished_at, created_at, updated_at, critical, high, medium, low, trivy_version, db_updated_at, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			started_at = excluded.started_at,
			finished_at = excluded.finished_at,
			updated_at = excluded.updated_at,
			critical = excluded.critical,
			high = excluded.high,
			medium = excluded.medium,
			low = excluded.low,
			trivy_version = excluded.trivy_version,
			db_updated_at = excluded.db_updated_at,
			error = excluded.error
	`, run.ID, tenant, run.Repository, run.RequestedRef, run.Digest, run.Status, run.Trigger, startedAt, finishedAt, run.CreatedAt.UTC().Format(time.RFC3339Nano), run.UpdatedAt.UTC().Format(time.RFC3339Nano), run.Critical, run.High, run.Medium, run.Low, run.TrivyVersion, dbUpdatedAt, run.Error)
	return err
}

func secretScanRunRow(row scanRunScanner) (ports.SecretScanRun, error) {
	run, err := secretScanRunFromScanner(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return ports.SecretScanRun{}, domain.NewNotFoundError("secret_scan_run", "")
		}
		return ports.SecretScanRun{}, err
	}
	return run, nil
}

func secretScanRunRows(rows *sql.Rows) (ports.SecretScanRun, error) {
	return secretScanRunFromScanner(rows)
}

func secretScanRunFromScanner(scanner scanRunScanner) (ports.SecretScanRun, error) {
	var run ports.SecretScanRun
	var startedAt sql.NullString
	var finishedAt sql.NullString
	var createdAtRaw string
	var updatedAtRaw string
	if err := scanner.Scan(&run.ID, &run.Repository, &run.Digest, &run.Status, &run.Trigger, &startedAt, &finishedAt, &createdAtRaw, &updatedAtRaw, &run.GitleaksVersion, &run.Error); err != nil {
		return ports.SecretScanRun{}, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return ports.SecretScanRun{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return ports.SecretScanRun{}, err
	}
	run.CreatedAt = createdAt
	run.UpdatedAt = updatedAt
	if run.StartedAt, err = parseOptionalTime(startedAt); err != nil {
		return ports.SecretScanRun{}, err
	}
	if run.FinishedAt, err = parseOptionalTime(finishedAt); err != nil {
		return ports.SecretScanRun{}, err
	}
	return run, nil
}

func upsertSecretScanRunTx(ctx context.Context, tx *sql.Tx, tenant string, run ports.SecretScanRun) error {
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = run.CreatedAt
	}
	var startedAt any
	if run.StartedAt != nil {
		startedAt = run.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	var finishedAt any
	if run.FinishedAt != nil {
		finishedAt = run.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO secret_scan_runs (id, tenant, repository, digest, status, trigger, started_at, finished_at, created_at, updated_at, gitleaks_version, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			started_at = excluded.started_at,
			finished_at = excluded.finished_at,
			updated_at = excluded.updated_at,
			gitleaks_version = excluded.gitleaks_version,
			error = excluded.error
	`, run.ID, tenant, run.Repository, run.Digest, run.Status, run.Trigger, startedAt, finishedAt, run.CreatedAt.UTC().Format(time.RFC3339Nano), run.UpdatedAt.UTC().Format(time.RFC3339Nano), run.GitleaksVersion, run.Error)
	return err
}

func (s *Store) listSecretScanFindings(ctx context.Context, runID string) ([]ports.SecretFinding, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT rule_id, description, blob_digest, path, start_line, end_line, tags
		FROM secret_scan_findings
		WHERE run_id = ?
		ORDER BY position ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	findings := make([]ports.SecretFinding, 0)
	for rows.Next() {
		var finding ports.SecretFinding
		var tagsJSON string
		if err := rows.Scan(&finding.RuleID, &finding.Description, &finding.BlobDigest, &finding.Path, &finding.StartLine, &finding.EndLine, &tagsJSON); err != nil {
			return nil, err
		}
		if strings.TrimSpace(tagsJSON) != "" {
			if err := json.Unmarshal([]byte(tagsJSON), &finding.Tags); err != nil {
				return nil, err
			}
		}
		findings = append(findings, finding)
	}
	return findings, rows.Err()
}

func (s *Store) secretScanRunFindingCount(ctx context.Context, runID string) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM secret_scan_findings WHERE run_id = ?`, runID)
	var count int
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func scanRunFromScanner(scanner scanRunScanner) (ports.ScanRun, error) {
	var run ports.ScanRun
	var startedAt sql.NullString
	var finishedAt sql.NullString
	var dbUpdatedAt sql.NullString
	var createdAtRaw string
	var updatedAtRaw string
	if err := scanner.Scan(&run.ID, &run.Repository, &run.RequestedRef, &run.Digest, &run.Status, &run.Trigger, &startedAt, &finishedAt, &createdAtRaw, &updatedAtRaw, &run.Critical, &run.High, &run.Medium, &run.Low, &run.TrivyVersion, &dbUpdatedAt, &run.Error); err != nil {
		return ports.ScanRun{}, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return ports.ScanRun{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return ports.ScanRun{}, err
	}
	run.CreatedAt = createdAt
	run.UpdatedAt = updatedAt
	if startedAt.Valid && strings.TrimSpace(startedAt.String) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, startedAt.String)
		if err != nil {
			return ports.ScanRun{}, err
		}
		run.StartedAt = &parsed
	}
	if finishedAt.Valid && strings.TrimSpace(finishedAt.String) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, finishedAt.String)
		if err != nil {
			return ports.ScanRun{}, err
		}
		run.FinishedAt = &parsed
	}
	if dbUpdatedAt.Valid && strings.TrimSpace(dbUpdatedAt.String) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, dbUpdatedAt.String)
		if err != nil {
			return ports.ScanRun{}, err
		}
		run.DBUpdatedAt = &parsed
	}
	return run, nil
}
