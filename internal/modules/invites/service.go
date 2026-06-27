package invites

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrForbidden       = errors.New("forbidden")
	ErrInvalidInput    = errors.New("invalid input")
	ErrConflictingRole = errors.New("conflicting role")
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

var allowedInviteTypes = map[string]bool{
	"MANAGER_INVITE":          true,
	"DRIVER_INVITE":           true,
	"CUSTOMER_INVITE":         true,
	"NURSERY_ONBOARDING_INVITE": true,
	"TRIP_SHARE_INVITE":       true,
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
		if hasRole(actor, role) {
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
			_ = s.repository.AddNurseryMember(ctx, *invite.NurseryID, actor.UserID, role, invite.InvitedByUserID)
		}
	case "NURSERY_ONBOARDING_INVITE":
		_ = s.repository.GrantNurseryOwnerRole(ctx, actor.UserID)
	}

	s.audit(ctx, actor, invite.ID, actionUpdate, map[string]any{"status": "ACCEPTED"})
	return *invite, nil
}

func (s *Service) Cancel(ctx context.Context, actor ActorContext, uuid string) (Invite, error) {
	invite, err := s.repository.FindByUUID(ctx, uuid)
	if err != nil {
		return Invite{}, err
	}
	if !hasRole(actor, "ADMIN") && !hasRole(actor, "SUPER_ADMIN") && invite.InvitedByUserID != actor.UserID {
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
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
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

func hasRole(actor ActorContext, role string) bool {
	for _, r := range actor.Roles {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	return false
}

func (s *Service) audit(ctx context.Context, actor ActorContext, recordID int64, action string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	_ = s.repository.CreateAuditLog(ctx, CreateAuditInput{
		TableName: "invites",
		RecordID:  recordID,
		Action:    action,
		ChangedBy: actor.UserID,
		SourceIP:  actor.IPAddress,
		UserAgent: actor.UserAgent,
		NewJSON:   string(b),
		At:        time.Now(),
	})
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(b)
}
