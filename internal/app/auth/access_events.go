package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// auditWriteTimeout bounds the best-effort history write that follows a
// mutation that already succeeded.
const auditWriteTimeout = 5 * time.Second

// ListAdminAccessEvents lists the newest access changes first, at most limit
// rows. Global admin only. Names and short text only, never secret material.
func (s *Service) ListAdminAccessEvents(ctx context.Context, actor domainauth.Principal, limit int) ([]ports.AdminAccessEvent, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}

	events, err := s.store.ListAccessEvents(ctx, limit)
	if err != nil {
		return nil, err
	}

	result := make([]ports.AdminAccessEvent, 0, len(events))
	for _, event := range events {
		result = append(result, ports.AdminAccessEvent{
			ID:         event.ID,
			At:         event.OccurredAt,
			Action:     event.Action,
			Actor:      event.ActorUsername,
			Subject:    event.SubjectUsername,
			Repository: event.Repository,
			Role:       event.Role,
			TokenName:  event.TokenName,
			Detail:     event.Detail,
		})
	}

	return result, nil
}

// recordAccessEvent appends one history row after a mutation succeeded. It is
// best effort by design: an audit write must never turn a successful change
// into an error, and the service has no logger, so a failure is dropped.
func (s *Service) recordAccessEvent(ctx context.Context, actor domainauth.Principal, event domainauth.AccessEvent) {
	event.ID = uuid.NewString()
	event.OccurredAt = s.now()
	event.ActorUserID = actor.UserID
	event.ActorUsername = actor.Username
	event = event.Normalized()

	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditWriteTimeout)
	defer cancel()
	_ = s.store.AppendAccessEvent(auditCtx, event)
}

// grantRoleOn returns the role the grants hold on the repository, if any.
func grantRoleOn(grants []domainauth.RepoGrant, repository string) (domainauth.RepoRole, bool) {
	for _, grant := range grants {
		if grant.Repository.String() == repository {
			return grant.Role, true
		}
	}

	return "", false
}

// recordGrantPut records a grant given or changed. previous is the role held
// before the write when it is known; a write that leaves the role unchanged
// records nothing.
func (s *Service) recordGrantPut(ctx context.Context, actor domainauth.Principal, subject domainauth.User, grant domainauth.RepoGrant, previous domainauth.RepoRole, hadPrevious bool) {
	detail := ""
	if hadPrevious {
		if previous == grant.Role {
			return
		}
		detail = roleLabel(previous) + " to " + roleLabel(grant.Role)
	}

	s.recordAccessEvent(ctx, actor, domainauth.AccessEvent{
		Action:          domainauth.AccessActionGrantPut,
		SubjectUserID:   subject.ID,
		SubjectUsername: subject.Username,
		Repository:      grant.Repository.String(),
		Role:            string(grant.Role),
		Detail:          detail,
	})
}

func (s *Service) recordGrantDelete(ctx context.Context, actor domainauth.Principal, subject domainauth.User, repository string) {
	s.recordAccessEvent(ctx, actor, domainauth.AccessEvent{
		Action:          domainauth.AccessActionGrantDelete,
		SubjectUserID:   subject.ID,
		SubjectUsername: subject.Username,
		Repository:      repository,
	})
}

func (s *Service) recordTokenIssued(ctx context.Context, actor domainauth.Principal, subject domainauth.User, token domainauth.Token, ttl time.Duration) {
	s.recordAccessEvent(ctx, actor, domainauth.AccessEvent{
		Action:          domainauth.AccessActionTokenIssued,
		SubjectUserID:   subject.ID,
		SubjectUsername: subject.Username,
		TokenName:       token.Name,
		Detail:          humanLifetime(ttl),
	})
}

func (s *Service) recordTokenRevoked(ctx context.Context, actor domainauth.Principal, subject domainauth.User, token domainauth.Token) {
	s.recordAccessEvent(ctx, actor, domainauth.AccessEvent{
		Action:          domainauth.AccessActionTokenRevoked,
		SubjectUserID:   subject.ID,
		SubjectUsername: subject.Username,
		TokenName:       token.Name,
	})
}

func (s *Service) recordRobotCreated(ctx context.Context, actor domainauth.Principal, robot domainauth.User, grant domainauth.RepoGrant, tokenName string, ttl time.Duration) {
	s.recordAccessEvent(ctx, actor, domainauth.AccessEvent{
		Action:          domainauth.AccessActionRobotCreated,
		SubjectUserID:   robot.ID,
		SubjectUsername: robot.Username,
		Repository:      grant.Repository.String(),
		Role:            string(grant.Role),
		TokenName:       tokenName,
		Detail:          humanLifetime(ttl),
	})
}

// roleLabel words a role for people: "repo-reader" becomes "Reader".
func roleLabel(role domainauth.RepoRole) string {
	name := strings.TrimPrefix(string(role), "repo-")
	if name == "" {
		return ""
	}

	return strings.ToUpper(name[:1]) + name[1:]
}

const (
	lifetimeHour = time.Hour
	lifetimeDay  = 24 * time.Hour
	lifetimeYear = 365 * lifetimeDay
)

// humanLifetime words a lifetime in its natural unit: years when it is a whole
// number of 365 day years, else days when whole, else hours. Never seconds.
func humanLifetime(d time.Duration) string {
	count, unit := int64(d/lifetimeHour), "hour"
	switch {
	case d > 0 && d%lifetimeYear == 0:
		count, unit = int64(d/lifetimeYear), "year"
	case d > 0 && d%lifetimeDay == 0:
		count, unit = int64(d/lifetimeDay), "day"
	}
	if count != 1 {
		unit += "s"
	}

	return fmt.Sprintf("%d %s", count, unit)
}
