package ports

import (
	"context"
	"time"
)

// ProjectSummary is one project with its derived totals. Repositories and
// Tags count the repositories whose FIRST path segment is the project (exact
// segment match, never a string prefix: "odabank" does not count
// "odabank2/..."). LastPushed is the zero time when nothing was pushed.
type ProjectSummary struct {
	Name         string
	CreatedAt    time.Time
	CreatedBy    string
	Repositories int
	Tags         int
	LastPushed   time.Time
}

// ProjectStore persists projects, per tenant. Names reaching it are already
// validated (domain.ParseProjectName). It is implemented by the SQLite
// metadata store and kept apart from MetadataStore: nothing on the push or
// read paths depends on it yet.
type ProjectStore interface {
	// CreateProject adds a project; a duplicate returns a conflict error
	// wrapping domain.ErrProjectExists.
	CreateProject(ctx context.Context, tenant string, name string, createdBy string) error
	ProjectExists(ctx context.Context, tenant string, name string) (bool, error)
	// ListProjects returns the tenant's projects ordered by name.
	ListProjects(ctx context.Context, tenant string) ([]ProjectSummary, error)
	// DeleteProjectIfEmpty removes a project only when it has no repository
	// and no open upload, in one write transaction. A missing project returns
	// a not found error; a non-empty one returns a conflict error wrapping
	// domain.ErrProjectNotEmpty.
	DeleteProjectIfEmpty(ctx context.Context, tenant string, name string) error
}

// ProjectGrantCleaner is the seam through which project grants (kept in the
// auth store, a different database) are purged when a project is deleted.
// It is declared here so the project service can call it, but wiring a real
// implementation belongs to the admin API unit; the service leaves it nil by
// default.
type ProjectGrantCleaner interface {
	PurgeProjectGrants(ctx context.Context, project string) error
}
