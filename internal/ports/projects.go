package ports

import (
	"context"
	"time"

	domainauth "regixtry/internal/domain/auth"
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
	// GetProject returns one project with the same totals ListProjects
	// computes for it; a missing project is a not found error.
	GetProject(ctx context.Context, tenant string, name string) (ProjectSummary, error)
	// ListProjects returns the tenant's projects ordered by name.
	ListProjects(ctx context.Context, tenant string) ([]ProjectSummary, error)
	// DeleteProjectIfEmpty removes a project only when it has no repository
	// and no open upload, in one write transaction. A missing project returns
	// a not found error; a non-empty one returns a conflict error wrapping
	// domain.ErrProjectNotEmpty.
	DeleteProjectIfEmpty(ctx context.Context, tenant string, name string) error
}

// ProjectGrantCleaner is the seam through which project grants (kept in the
// auth store, a different database) are purged around a project's life: before
// a name is (re)created and after a project is deleted. It returns how many
// grants it removed. The auth service implements it; the project service
// leaves it nil until main wires it.
type ProjectGrantCleaner interface {
	PurgeProjectGrants(ctx context.Context, project string) (int64, error)
}

// ProjectEventRecorder is the seam through which the project service writes
// the access events project.created and project.deleted into the auth
// history. Recording is best effort and never fails the operation.
type ProjectEventRecorder interface {
	RecordProjectEvent(ctx context.Context, actor domainauth.Principal, action domainauth.AccessAction, project string)
}

// ProjectLookup lets the auth service ask whether a project exists without
// importing registry code. The project is already validated.
type ProjectLookup interface {
	ProjectExists(ctx context.Context, project string) (bool, error)
}

// ProjectAdmin is what the admin HTTP API needs from the project service.
// Every method is restricted to global administrators by the implementation.
type ProjectAdmin interface {
	CreateProject(ctx context.Context, name string) error
	DeleteProject(ctx context.Context, name string) error
	ListProjects(ctx context.Context) ([]ProjectSummary, error)
	GetProject(ctx context.Context, name string) (ProjectSummary, error)
}
