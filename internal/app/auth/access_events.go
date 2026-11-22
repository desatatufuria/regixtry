package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// auditWriteTimeout bounds the best-effort history write that follows a
// mutation that already succeeded.
const auditWriteTimeout = 2 * time.Second

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
// into an error, so a failure is only logged.
func (s *Service) recordAccessEvent(ctx context.Context, actor domainauth.Principal, event domainauth.AccessEvent) {
	event.ID = uuid.NewString()
	event.OccurredAt = s.now()
	event.ActorUserID = actor.UserID
	event.ActorUsername = actor.Username
	event = event.Normalized()

	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditWriteTimeout)
	defer cancel()
	if err := s.store.AppendAccessEvent(auditCtx, event); err != nil {
		// Only the action and the error text: never a token name, detail,
		// secret or any other field of the event.
		slog.Warn("access event not recorded", "action", string(event.Action), "error", err.Error())
	}
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

// recordProjectGrantPut records a project grant given or changed, worded like
// recordGrantPut; the project is stored as "project:<name>" in the repository
// column (the history table has no project column).
func (s *Service) recordProjectGrantPut(ctx context.Context, actor domainauth.Principal, subject domainauth.User, grant domainauth.ProjectGrant, previous domainauth.RepoRole, hadPrevious bool) {
	detail := ""
	if hadPrevious {
		if previous == grant.Role {
			return
		}
		detail = roleLabel(previous) + " to " + roleLabel(grant.Role)
	}

	s.recordAccessEvent(ctx, actor, domainauth.AccessEvent{
		Action:          domainauth.AccessActionProjectGrantPut,
		SubjectUserID:   subject.ID,
		SubjectUsername: subject.Username,
		Repository:      domainauth.ProjectEventRepository(grant.Project),
		Role:            string(grant.Role),
		Detail:          detail,
	})
}

func (s *Service) recordProjectGrantDelete(ctx context.Context, actor domainauth.Principal, subject domainauth.User, project string) {
	s.recordAccessEvent(ctx, actor, domainauth.AccessEvent{
		Action:          domainauth.AccessActionProjectGrantDelete,
		SubjectUserID:   subject.ID,
		SubjectUsername: subject.Username,
		Repository:      domainauth.ProjectEventRepository(project),
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

// userRoleFlags words the flags a new user is created with.
func userRoleFlags(user domainauth.User) string {
	var flags []string
	if user.IsAdmin {
		flags = append(flags, "administrator")
	}
	if user.IsReadOnly {
		flags = append(flags, "read-only")
	}
	if len(flags) == 0 {
		return "standard user"
	}

	return strings.Join(flags, ", ")
}

func (s *Service) recordUserCreated(ctx context.Context, actor domainauth.Principal, user domainauth.User) {
	s.recordUserEvent(ctx, actor, domainauth.AccessActionUserCreated, user, userRoleFlags(user))
}

// recordUserUpdated records the flags that changed between before and after;
// an update that changes none records nothing.
func (s *Service) recordUserUpdated(ctx context.Context, actor domainauth.Principal, before domainauth.User, after domainauth.User) {
	var changes []string
	if before.IsAdmin != after.IsAdmin {
		changes = append(changes, "administrator "+onOff(after.IsAdmin))
	}
	if before.IsReadOnly != after.IsReadOnly {
		changes = append(changes, "read-only "+onOff(after.IsReadOnly))
	}
	if len(changes) == 0 {
		return
	}

	s.recordUserEvent(ctx, actor, domainauth.AccessActionUserUpdated, after, strings.Join(changes, ", "))
}

func onOff(on bool) string {
	if on {
		return "on"
	}

	return "off"
}

// recordUserEvent records a user lifecycle change. The subject name is kept in
// the event itself because the history has no foreign keys and must outlive a
// deleted user.
func (s *Service) recordUserEvent(ctx context.Context, actor domainauth.Principal, action domainauth.AccessAction, subject domainauth.User, detail string) {
	s.recordAccessEvent(ctx, actor, domainauth.AccessEvent{
		Action:          action,
		SubjectUserID:   subject.ID,
		SubjectUsername: subject.Username,
		Detail:          detail,
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
// number of 365 day years, else days when whole, else hours; below one hour
// whole minutes, and below one minute "less than 1 minute". Never seconds.
func humanLifetime(d time.Duration) string {
	if d < lifetimeHour {
		if d < time.Minute {
			return "less than 1 minute"
		}
		minutes := int64(d / time.Minute)
		if minutes == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", minutes)
	}

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
