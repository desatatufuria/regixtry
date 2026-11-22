package auth

import (
	"context"
	"strings"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// SetProjectLookup installs the directory the service asks whether a project
// exists (project grants for a missing project are refused). It is the one
// seam to the registry metadata store, so this package never imports registry
// code. Set it once at startup, before the service handles requests; without
// it project grants are unavailable (unsupported), never silently accepted.
func (s *Service) SetProjectLookup(lookup ports.ProjectLookup) {
	s.projects = lookup
}

// projectMustExist is the existence check of a project grant. It runs AFTER
// authorization, so a caller without rights gets the same refusal whether or
// not the project exists and never learns which projects there are.
func (s *Service) projectMustExist(ctx context.Context, project string) error {
	if s.projects == nil {
		return regixtrydomain.NewUnsupportedError("projects are not available in this deployment")
	}
	exists, err := s.projects.ProjectExists(ctx, project)
	if err != nil {
		return err
	}
	if !exists {
		return domainauth.NewNotFoundError("project", project)
	}

	return nil
}

// PutProjectGrant gives a user or robot one role over every repository of a
// project, or changes it. Grants are addressed by username so no caller needs
// to read the user directory. The role is validated like RepoRole; the
// project must exist.
func (s *Service) PutProjectGrant(ctx context.Context, actor domainauth.Principal, project string, username string, role domainauth.RepoRole) (domainauth.ProjectGrant, error) {
	project, err := regixtrydomain.ParseProjectName(strings.TrimSpace(project))
	if err != nil {
		return domainauth.ProjectGrant{}, err
	}
	if err := requireAdminOrProjectAdmin(actor, project); err != nil {
		return domainauth.ProjectGrant{}, err
	}
	if err := role.Validate(); err != nil {
		return domainauth.ProjectGrant{}, err
	}
	// Escalation bound: only a global admin hands out project admin. This
	// also covers a delegate granting it to themselves.
	if !actor.IsAdmin && role == domainauth.RepoRoleAdmin {
		return domainauth.ProjectGrant{}, domainauth.NewForbiddenError("project administrators cannot grant project admin")
	}
	if err := s.projectMustExist(ctx, project); err != nil {
		return domainauth.ProjectGrant{}, err
	}

	target, err := s.store.GetUserByUsername(ctx, normalizeUsername(username))
	if err != nil {
		return domainauth.ProjectGrant{}, err
	}
	// A robot token is an admin credential: a project admin robot would
	// delegate project management, and a robot holds a single grant.
	if target.IsRobot {
		if role == domainauth.RepoRoleAdmin {
			return domainauth.ProjectGrant{}, domainauth.NewValidationError("robots on a project can be reader or writer only")
		}
		repoGrants, err := s.store.ListRepoGrants(ctx, target.ID)
		if err != nil {
			return domainauth.ProjectGrant{}, err
		}
		if len(repoGrants) > 0 {
			return domainauth.ProjectGrant{}, domainauth.NewConflictError(robotOneGrantMessage)
		}
		held, err := s.store.ListProjectGrants(ctx, target.ID)
		if err != nil {
			return domainauth.ProjectGrant{}, err
		}
		for _, grant := range held {
			if grant.Project != project {
				return domainauth.ProjectGrant{}, domainauth.NewConflictError(robotOneGrantMessage)
			}
		}
	}
	// A read-only user must never gain write or administration through a
	// project: only the reader role is allowed.
	if target.IsReadOnly && role != domainauth.RepoRoleReader {
		return domainauth.ProjectGrant{}, domainauth.NewValidationError("read-only users can only be project readers")
	}

	// The existing grants bound a delegate and word the access history
	// ("Reader to Writer"). For a global admin a failed read never blocks the
	// grant; for a delegate it still does.
	existing, listErr := s.store.ListProjectGrants(ctx, target.ID)
	if !actor.IsAdmin {
		if listErr != nil {
			return domainauth.ProjectGrant{}, listErr
		}
		if current, ok := projectGrantRoleOn(existing, project); ok && current == domainauth.RepoRoleAdmin {
			return domainauth.ProjectGrant{}, domainauth.NewForbiddenError("project administrators cannot modify another project administrator's grant")
		}
	}
	previous, hadPrevious := projectGrantRoleOn(existing, project)

	now := s.now()
	grant := domainauth.ProjectGrant{UserID: target.ID, Project: project, Role: role, CreatedAt: now, UpdatedAt: now}
	if err := s.store.PutProjectGrant(ctx, grant); err != nil {
		return domainauth.ProjectGrant{}, err
	}

	if listErr == nil {
		s.recordProjectGrantPut(ctx, actor, target, grant, previous, hadPrevious)
	}

	return grant, nil
}

// PutAdminProjectGrant is the HTTP-facing PutProjectGrant: it answers with the
// member projection, never a user ID.
func (s *Service) PutAdminProjectGrant(ctx context.Context, actor domainauth.Principal, input ports.AdminPutProjectGrantInput) (ports.AdminProjectMember, error) {
	grant, err := s.PutProjectGrant(ctx, actor, input.Project, input.Username, input.Role)
	if err != nil {
		return ports.AdminProjectMember{}, err
	}
	target, err := s.store.GetUserByID(ctx, grant.UserID)
	if err != nil {
		return ports.AdminProjectMember{}, err
	}

	return toAdminProjectMember(target, grant), nil
}

// DeleteProjectGrant revokes one user's grant on a project. It does not
// require the project to exist, so an orphan grant left by a failed cleanup
// can still be removed; exact repository grants are never touched.
func (s *Service) DeleteProjectGrant(ctx context.Context, actor domainauth.Principal, project string, username string) error {
	project, err := regixtrydomain.ParseProjectName(strings.TrimSpace(project))
	if err != nil {
		return err
	}
	if err := requireAdminOrProjectAdmin(actor, project); err != nil {
		return err
	}

	target, err := s.store.GetUserByUsername(ctx, normalizeUsername(username))
	if err != nil {
		return err
	}
	if !actor.IsAdmin {
		existing, err := s.store.ListProjectGrants(ctx, target.ID)
		if err != nil {
			return err
		}
		if current, ok := projectGrantRoleOn(existing, project); ok && current == domainauth.RepoRoleAdmin {
			return domainauth.NewForbiddenError("project administrators cannot modify another project administrator's grant")
		}
	}
	if err := s.store.DeleteProjectGrant(ctx, target.ID, project); err != nil {
		return err
	}

	s.recordProjectGrantDelete(ctx, actor, target, project)

	return nil
}

// DeleteAdminProjectGrant is the HTTP-facing DeleteProjectGrant.
func (s *Service) DeleteAdminProjectGrant(ctx context.Context, actor domainauth.Principal, project string, username string) error {
	return s.DeleteProjectGrant(ctx, actor, project, username)
}

// PurgeProjectGrants removes every grant of a project and reports how many it
// removed. It is the ports.ProjectGrantCleaner seam the project service calls
// around a project's life; it does its own no authorization because only the
// project service (global admin) reaches it.
func (s *Service) PurgeProjectGrants(ctx context.Context, project string) (int64, error) {
	return s.store.DeleteProjectGrantsByProject(ctx, project)
}

// RecordProjectEvent is the ports.ProjectEventRecorder seam: the project
// service records project.created and project.deleted through the same
// history as every other access change. Any other action is ignored.
func (s *Service) RecordProjectEvent(ctx context.Context, actor domainauth.Principal, action domainauth.AccessAction, project string) {
	if action != domainauth.AccessActionProjectCreated && action != domainauth.AccessActionProjectDeleted {
		return
	}

	s.recordAccessEvent(ctx, actor, domainauth.AccessEvent{
		Action:     action,
		Repository: domainauth.ProjectEventRepository(project),
	})
}

const robotOneGrantMessage = "robot already has a grant; a robot holds one grant"

// refuseRepoGrantOnProjectRobot keeps the one-grant invariant from the
// repository side: a robot bound to a project takes no exact repository grant
// (which also keeps it from ever reaching repo-admin). Users and repository
// robots are untouched.
func (s *Service) refuseRepoGrantOnProjectRobot(ctx context.Context, target domainauth.User) error {
	if !target.IsRobot {
		return nil
	}
	held, err := s.store.ListProjectGrants(ctx, target.ID)
	if err != nil {
		return err
	}
	if len(held) > 0 {
		return domainauth.NewConflictError(robotOneGrantMessage)
	}

	return nil
}

func projectGrantRoleOn(grants []domainauth.ProjectGrant, project string) (domainauth.RepoRole, bool) {
	for _, grant := range grants {
		if grant.Project == project {
			return grant.Role, true
		}
	}

	return "", false
}

func toAdminProjectMember(user domainauth.User, grant domainauth.ProjectGrant) ports.AdminProjectMember {
	return ports.AdminProjectMember{Username: user.Username, IsRobot: user.IsRobot, Role: grant.Role, CreatedAt: grant.CreatedAt, UpdatedAt: grant.UpdatedAt}
}
