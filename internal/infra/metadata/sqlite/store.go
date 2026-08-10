package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
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

func (s *Store) GetScanSettings(ctx context.Context, tenant string) (ports.ScanSettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT enabled, schedule_enabled, interval, timeout, cache_dir, binary_path, service_url, registry_reachable_url, auth_token, tls_ca_cert_path, tls_insecure_skip_verify, max_concurrency, updated_at
		FROM scan_settings
		WHERE tenant = ?
	`, tenant)
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

func (s *Store) UpsertScanSettings(ctx context.Context, tenant string, settings ports.ScanSettings) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO scan_settings (tenant, enabled, schedule_enabled, interval, timeout, cache_dir, binary_path, service_url, registry_reachable_url, auth_token, tls_ca_cert_path, tls_insecure_skip_verify, max_concurrency, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant) DO UPDATE SET
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
	`, tenant, settings.Enabled, settings.ScheduleEnabled, settings.Interval.String(), settings.Timeout.String(), settings.LegacyCacheDir, settings.LegacyBinaryPath, settings.ServiceURL, settings.RegistryReachableURL, settings.AuthToken, settings.TLSCACertPath, settings.TLSInsecureSkipVerify, settings.MaxConcurrency, settings.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetTrivyRuntimeState(ctx context.Context, tenant string) (ports.TrivyRuntimeState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT status, active_version, previous_version, active_binary_path, cache_dir, receipt_path, migration_hint, last_verified_at, last_health_check_at, last_db_updated_at, last_error, updated_at
		FROM trivy_runtime_state
		WHERE tenant = ?
	`, tenant)
	state, err := scanTrivyRuntimeStateRow(row)
	if err == nil {
		return state, nil
	}
	if err != sql.ErrNoRows {
		return ports.TrivyRuntimeState{}, err
	}
	settings, settingsErr := s.GetScanSettings(ctx, tenant)
	if settingsErr != nil {
		if domain.IsCode(settingsErr, domain.ErrorCodeNotFound) {
			return ports.TrivyRuntimeState{}, domain.NewNotFoundError("trivy_runtime_state", tenant)
		}
		return ports.TrivyRuntimeState{}, settingsErr
	}
	if legacyState, ok := deriveLegacyTrivyRuntimeState(settings); ok {
		return legacyState, nil
	}
	return ports.TrivyRuntimeState{}, domain.NewNotFoundError("trivy_runtime_state", tenant)
}

func (s *Store) UpsertTrivyRuntimeState(ctx context.Context, tenant string, state ports.TrivyRuntimeState) error {
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
		INSERT INTO trivy_runtime_state (tenant, status, active_version, previous_version, active_binary_path, cache_dir, receipt_path, migration_hint, last_verified_at, last_health_check_at, last_db_updated_at, last_error, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant) DO UPDATE SET
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
	`, tenant, string(state.Status), state.ActiveVersion, state.PreviousVersion, state.ActiveBinaryPath, state.CacheDir, state.ReceiptPath, state.MigrationHint, lastVerifiedAt, lastHealthAt, lastDBUpdated, state.LastError, state.UpdatedAt.UTC().Format(time.RFC3339Nano))
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

func (s *Store) GetScanRun(ctx context.Context, tenant string, runID string) (ports.ScanRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, repository, requested_ref, digest, status, trigger, started_at, finished_at, created_at, updated_at, critical, high, medium, low, trivy_version, db_updated_at, error
		FROM scan_runs
		WHERE tenant = ? AND id = ?
	`, tenant, runID)
	return scanRunRow(row)
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
	query += ` ORDER BY created_at DESC`
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
		`CREATE TABLE IF NOT EXISTS scan_settings (
			tenant TEXT PRIMARY KEY,
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
			updated_at TEXT NOT NULL
		);`,
		`ALTER TABLE scan_settings ADD COLUMN service_url TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE scan_settings ADD COLUMN registry_reachable_url TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE scan_settings ADD COLUMN auth_token TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE scan_settings ADD COLUMN tls_ca_cert_path TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE scan_settings ADD COLUMN tls_insecure_skip_verify INTEGER NOT NULL DEFAULT 0;`,
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

func deriveLegacyTrivyRuntimeState(settings ports.ScanSettings) (ports.TrivyRuntimeState, bool) {
	legacyBinary := strings.TrimSpace(settings.LegacyBinaryPath)
	legacyCache := strings.TrimSpace(settings.LegacyCacheDir)
	legacyService := strings.TrimSpace(settings.ServiceURL)
	if legacyBinary == "" && legacyCache == "" && legacyService == "" {
		return ports.TrivyRuntimeState{}, false
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
	return ports.TrivyRuntimeState{
		Status:        ports.TrivyRuntimeStatusMigrationRequired,
		MigrationHint: strings.Join(hintParts, "; "),
		UpdatedAt:     settings.UpdatedAt,
	}, true
}

func scanTrivyRuntimeStateRow(row scanRunScanner) (ports.TrivyRuntimeState, error) {
	var (
		statusRaw         string
		activeVersion     string
		previousVersion   string
		activeBinaryPath  string
		cacheDir          string
		receiptPath       string
		migrationHint     string
		lastVerifiedRaw   sql.NullString
		lastHealthRaw     sql.NullString
		lastDBUpdatedRaw  sql.NullString
		lastError         string
		updatedAtRaw      string
	)
	if err := row.Scan(&statusRaw, &activeVersion, &previousVersion, &activeBinaryPath, &cacheDir, &receiptPath, &migrationHint, &lastVerifiedRaw, &lastHealthRaw, &lastDBUpdatedRaw, &lastError, &updatedAtRaw); err != nil {
		return ports.TrivyRuntimeState{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return ports.TrivyRuntimeState{}, err
	}
	state := ports.TrivyRuntimeState{
		Status:           ports.TrivyRuntimeStatus(statusRaw),
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
		return ports.TrivyRuntimeState{}, err
	}
	if state.LastHealthCheckAt, err = parseOptionalTime(lastHealthRaw); err != nil {
		return ports.TrivyRuntimeState{}, err
	}
	if state.LastDBUpdatedAt, err = parseOptionalTime(lastDBUpdatedRaw); err != nil {
		return ports.TrivyRuntimeState{}, err
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
