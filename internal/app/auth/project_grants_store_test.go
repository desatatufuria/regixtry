package auth

import (
	"context"
	"sort"

	domainauth "regixtry/internal/domain/auth"
)

func (s *memoryAuthStore) PutProjectGrant(_ context.Context, grant domainauth.ProjectGrant) error {
	if s.projectGrants == nil {
		s.projectGrants = map[string][]domainauth.ProjectGrant{}
	}
	existing := s.projectGrants[grant.UserID]
	for i, current := range existing {
		if current.Project == grant.Project {
			grant.CreatedAt = current.CreatedAt
			existing[i] = grant
			return nil
		}
	}
	s.projectGrants[grant.UserID] = append(existing, grant)
	return nil
}

func (s *memoryAuthStore) DeleteProjectGrant(_ context.Context, userID string, project string) error {
	existing := s.projectGrants[userID]
	for i, current := range existing {
		if current.Project == project {
			s.projectGrants[userID] = append(existing[:i:i], existing[i+1:]...)
			return nil
		}
	}
	return domainauth.NewNotFoundError("project grant", project)
}

func (s *memoryAuthStore) ListProjectGrants(_ context.Context, userID string) ([]domainauth.ProjectGrant, error) {
	return append([]domainauth.ProjectGrant(nil), s.projectGrants[userID]...), nil
}

func (s *memoryAuthStore) ListProjectGrantsByProject(_ context.Context, project string, afterUserID string, limit int) ([]domainauth.ProjectGrant, error) {
	grants := make([]domainauth.ProjectGrant, 0)
	for _, userGrants := range s.projectGrants {
		for _, grant := range userGrants {
			if grant.Project == project && grant.UserID > afterUserID {
				grants = append(grants, grant)
			}
		}
	}
	sort.Slice(grants, func(i, j int) bool { return grants[i].UserID < grants[j].UserID })
	if limit < len(grants) {
		grants = grants[:max(limit, 0)]
	}
	return grants, nil
}

func (s *memoryAuthStore) DeleteProjectGrantsByProject(_ context.Context, project string) (int64, error) {
	var removed int64
	for userID, userGrants := range s.projectGrants {
		kept := userGrants[:0:0]
		for _, grant := range userGrants {
			if grant.Project == project {
				removed++
				continue
			}
			kept = append(kept, grant)
		}
		s.projectGrants[userID] = kept
	}
	return removed, nil
}
