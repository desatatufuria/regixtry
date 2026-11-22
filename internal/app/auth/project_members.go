package auth

import (
	"context"
	"encoding/base64"
	"strings"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

const projectMembersMaxLimit = 200

// ListProjectMembers lists the users and robots holding a grant on the
// project, ordered by user id, at most limit per page. after is the opaque
// Next of the previous page; Next is empty on the last page. Names, roles and
// timestamps only.
func (s *Service) ListProjectMembers(ctx context.Context, actor domainauth.Principal, project string, limit int, after string) (ports.AdminProjectMemberPage, error) {
	project, err := regixtrydomain.ParseProjectName(strings.TrimSpace(project))
	if err != nil {
		return ports.AdminProjectMemberPage{}, err
	}
	if err := requireAdmin(actor); err != nil {
		return ports.AdminProjectMemberPage{}, err
	}
	if limit < 1 || limit > projectMembersMaxLimit {
		return ports.AdminProjectMemberPage{}, domainauth.NewValidationError("limit must be a whole number between 1 and 200")
	}
	afterUserID, err := decodeMemberCursor(after)
	if err != nil {
		return ports.AdminProjectMemberPage{}, err
	}
	if err := s.projectMustExist(ctx, project); err != nil {
		return ports.AdminProjectMemberPage{}, err
	}

	// One extra row tells whether another page exists.
	grants, err := s.store.ListProjectGrantsByProject(ctx, project, afterUserID, limit+1)
	if err != nil {
		return ports.AdminProjectMemberPage{}, err
	}

	page := ports.AdminProjectMemberPage{Members: make([]ports.AdminProjectMember, 0, min(len(grants), limit))}
	if len(grants) > limit {
		grants = grants[:limit]
		page.Next = encodeMemberCursor(grants[limit-1].UserID)
	}
	for _, grant := range grants {
		user, err := s.store.GetUserByID(ctx, grant.UserID)
		if err != nil {
			if domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
				// The user was deleted after the grant page was read.
				continue
			}
			return ports.AdminProjectMemberPage{}, err
		}
		page.Members = append(page.Members, toAdminProjectMember(user, grant))
	}

	return page, nil
}

// ListAdminProjectMembers is the HTTP-facing ListProjectMembers.
func (s *Service) ListAdminProjectMembers(ctx context.Context, actor domainauth.Principal, project string, limit int, after string) (ports.AdminProjectMemberPage, error) {
	return s.ListProjectMembers(ctx, actor, project, limit, after)
}

// ListUserProjectGrants lists every project grant of one user, ordered by
// project. Global admin only (it names the user by id).
func (s *Service) ListUserProjectGrants(ctx context.Context, actor domainauth.Principal, userID string) ([]domainauth.ProjectGrant, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}
	if _, err := s.store.GetUserByID(ctx, userID); err != nil {
		return nil, err
	}

	return s.store.ListProjectGrants(ctx, userID)
}

// ListAdminUserProjectGrants is the HTTP-facing ListUserProjectGrants.
func (s *Service) ListAdminUserProjectGrants(ctx context.Context, actor domainauth.Principal, userID string) ([]ports.AdminProjectGrant, error) {
	grants, err := s.ListUserProjectGrants(ctx, actor, userID)
	if err != nil {
		return nil, err
	}

	result := make([]ports.AdminProjectGrant, 0, len(grants))
	for _, grant := range grants {
		result = append(result, ports.AdminProjectGrant{Project: grant.Project, Role: grant.Role, CreatedAt: grant.CreatedAt, UpdatedAt: grant.UpdatedAt})
	}

	return result, nil
}

func encodeMemberCursor(userID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(userID))
}

func decodeMemberCursor(after string) (string, error) {
	if after == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(after)
	if err != nil || len(raw) == 0 || len(raw) > 128 {
		return "", domainauth.NewValidationError("after is not a valid page cursor")
	}

	return string(raw), nil
}
