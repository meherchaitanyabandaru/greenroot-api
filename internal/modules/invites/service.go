package invites

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/redis/go-redis/v9"
)

var (
	ErrForbidden       = apperrs.ErrForbidden
	ErrInvalidInput    = apperrs.ErrInvalidInput
	ErrConflictingRole = errors.New("conflicting role")
	ErrAlreadyMember   = errors.New("already member of another nursery")
	ErrWrongTarget     = errors.New("invite addressed to a different person")
)

type Service struct {
	repository Repository
	auditSvc   *auditlog.Service
	redis      redis.Cmdable
}

func NewService(repository Repository, auditSvc *auditlog.Service, redisClients ...redis.Cmdable) *Service {
	var rdb redis.Cmdable
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	return &Service{repository: repository, auditSvc: auditSvc, redis: rdb}
}

var allowedInviteTypes = map[string]bool{
	"MANAGER_INVITE":            true,
	"DRIVER_INVITE":             true,
	"CUSTOMER_INVITE":           true,
	"NURSERY_ONBOARDING_INVITE": true,
	"TRIP_SHARE_INVITE":         true,
}

// inviteTypeAllowedRoles maps each invite type to the roles that may create it.
var inviteTypeAllowedRoles = map[string][]string{
	"MANAGER_INVITE":            {"ADMIN", "SUPER_ADMIN", "NURSERY_OWNER"},
	"DRIVER_INVITE":             {"ADMIN", "SUPER_ADMIN", "NURSERY_OWNER", "MANAGER"},
	"CUSTOMER_INVITE":           {"ADMIN", "SUPER_ADMIN", "NURSERY_OWNER", "MANAGER"},
	"NURSERY_ONBOARDING_INVITE": {"ADMIN", "SUPER_ADMIN"},
	"TRIP_SHARE_INVITE":         {"ADMIN", "SUPER_ADMIN", "NURSERY_OWNER", "MANAGER"},
}

func (s *Service) Create(ctx context.Context, actor ActorContext, req CreateInviteRequest) (Invite, error) {
	inviteType := strings.ToUpper(strings.TrimSpace(req.InviteType))
	if !allowedInviteTypes[inviteType] {
		return Invite{}, ErrInvalidInput
	}
	req.InviteType = inviteType

	// Enforce role-based invite creation: only allowed roles may create each type
	allowed := false
	for _, role := range inviteTypeAllowedRoles[inviteType] {
		if actor.HasRole(role) {
			allowed = true
			break
		}
	}
	if !allowed {
		return Invite{}, ErrForbidden
	}

	if (req.TargetMobile == nil || *req.TargetMobile == "") && (req.TargetEmail == nil || *req.TargetEmail == "") {
		return Invite{}, ErrInvalidInput
	}
	invite, err := s.repository.Create(ctx, actor.UserID, req)
	if err != nil {
		return Invite{}, err
	}
	s.audit(ctx, actor, invite.ID, actionInsert, req)
	return *invite, nil
}

func (s *Service) GetByUUID(ctx context.Context, uuid string) (Invite, error) {
	invite, err := s.repository.FindByUUID(ctx, uuid)
	if err != nil {
		return Invite{}, err
	}
	return *invite, nil
}

func (s *Service) Accept(ctx context.Context, actor ActorContext, uuid string) (Invite, error) {
	// Peek at the invite before accepting so we can enforce role exclusivity.
	pending, err := s.repository.FindByUUID(ctx, uuid)
	if err != nil {
		return Invite{}, err
	}

	// Enforce target: if the invite was addressed to a specific mobile number,
	// only that person may accept it — UUID secrecy is not enough on its own.
	if pending.TargetMobile != nil && *pending.TargetMobile != "" && actor.Mobile != "" {
		if actor.Mobile != *pending.TargetMobile {
			return Invite{}, ErrWrongTarget
		}
	}

	switch pending.InviteType {
	case "MANAGER_INVITE":
		// Nursery owners cannot accept a manager invite (conflicting roles).
		owns, err := s.repository.UserOwnsNursery(ctx, actor.UserID)
		if err != nil {
			return Invite{}, err
		}
		if owns {
			return Invite{}, ErrConflictingRole
		}
		// Managers can only work at one nursery at a time.
		isManager, err := s.repository.UserIsManager(ctx, actor.UserID)
		if err != nil {
			return Invite{}, err
		}
		if isManager {
			return Invite{}, ErrAlreadyMember
		}
	case "NURSERY_ONBOARDING_INVITE":
		// Managers cannot become nursery owners.
		isManager, err := s.repository.UserIsManager(ctx, actor.UserID)
		if err != nil {
			return Invite{}, err
		}
		if isManager {
			return Invite{}, ErrConflictingRole
		}
	}

	invite, err := s.repository.Accept(ctx, uuid, actor.UserID)
	if err != nil {
		return Invite{}, err
	}

	// Post-acceptance side effects
	switch invite.InviteType {
	case "MANAGER_INVITE":
		if invite.NurseryID != nil {
			role := "MANAGER"
			if invite.Role != nil && *invite.Role != "" {
				role = *invite.Role
			}
			if err := s.repository.AddNurseryMember(ctx, *invite.NurseryID, actor.UserID, role, invite.InvitedByUserID); err != nil {
				// Roll back: mark invite pending again so it can be retried after leaving the current nursery.
				_, _ = s.repository.Cancel(ctx, uuid, actor.UserID)
				return Invite{}, ErrAlreadyMember
			}
		}
	case "NURSERY_ONBOARDING_INVITE":
		_ = s.repository.GrantNurseryOwnerRole(ctx, actor.UserID)
	}

	s.invalidateWorkspaces(ctx, actor.UserID, invite.InvitedByUserID)
	s.audit(ctx, actor, invite.ID, actionUpdate, map[string]any{"status": "ACCEPTED"})
	return *invite, nil
}

func (s *Service) Cancel(ctx context.Context, actor ActorContext, uuid string) (Invite, error) {
	invite, err := s.repository.FindByUUID(ctx, uuid)
	if err != nil {
		return Invite{}, err
	}
	if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") && invite.InvitedByUserID != actor.UserID {
		return Invite{}, ErrForbidden
	}
	updated, err := s.repository.Cancel(ctx, uuid, actor.UserID)
	if err != nil {
		return Invite{}, err
	}
	s.audit(ctx, actor, updated.ID, actionUpdate, map[string]any{"status": "CANCELLED"})
	return *updated, nil
}

func (s *Service) ListByNursery(ctx context.Context, actor ActorContext, nurseryID int64) ([]Invite, error) {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return s.repository.ListByNursery(ctx, nurseryID)
	}
	// Nursery owner can list invites for their own nursery.
	isOwner, err := s.repository.IsNurseryOwner(ctx, nurseryID, actor.UserID)
	if err != nil {
		return nil, err
	}
	if !isOwner {
		return nil, ErrForbidden
	}
	return s.repository.ListByNursery(ctx, nurseryID)
}

func (s *Service) ListMyConnections(ctx context.Context, actor ActorContext) ([]Invite, error) {
	return s.repository.ListAcceptedByUser(ctx, actor.UserID)
}

func (s *Service) invalidateWorkspaces(ctx context.Context, userIDs ...int64) {
	redisutil.InvalidateWorkspaces(ctx, s.redis, slog.Default(), userIDs...)
}

func (s *Service) audit(ctx context.Context, actor ActorContext, entityID int64, action auditlog.Action, newValue any) {
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:     actor.UserID,
		Module:     auditlog.ModuleInvites,
		EntityType: "invite",
		EntityID:   entityID,
		Action:     action,
		NewValue:   newValue,
		IPAddress:  actor.IPAddress,
		DeviceInfo: actor.UserAgent,
	})
}
