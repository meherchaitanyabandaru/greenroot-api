package users

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/revocation"
	platformstorage "github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
)

var (
	ErrForbidden              = errors.New("forbidden")
	ErrInvalidInput           = errors.New("invalid input")
	ErrInvalidAddress         = errors.New("invalid address")
	ErrAccountDeleted         = errors.New("account has already been deleted")
	ErrAccountDeletionBlocked = errors.New("account deletion blocked by active business records")
)

const defaultUserFirstName = "GreenRoot"

type Service struct {
	repository Repository
	storage    *platformstorage.Client
	auditSvc   *auditlog.Service
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}

func NewService(repository Repository, storage *platformstorage.Client, auditSvc *auditlog.Service) *Service {
	return &Service{repository: repository, storage: storage, auditSvc: auditSvc}
}

func (s *Service) Me(ctx context.Context, actor ActorContext) (User, error) {
	return s.GetUser(ctx, actor, actor.UserID)
}

func (s *Service) UpdateMe(ctx context.Context, actor ActorContext, input UpdateProfileRequest) (User, error) {
	input.FirstName = trimString(input.FirstName)
	if input.FirstName == "" {
		return User{}, ErrInvalidInput
	}
	if input.Gender != nil {
		*input.Gender = strings.ToUpper(trimString(*input.Gender))
		if !isAllowedGender(*input.Gender) {
			return User{}, ErrInvalidInput
		}
	}

	now := time.Now()
	user, err := s.repository.UpdateProfile(ctx, actor.UserID, input, now)
	if err != nil {
		return User{}, err
	}

	s.recordChange(ctx, actor, "users", user.ID, "UPDATE", activityUpdateProfile, "USER", user.ID, map[string]any{
		"first_name":        input.FirstName,
		"last_name_changed": input.LastName != nil,
		"email_changed":     input.Email != nil,
		"profile_changed":   input.ProfileImageURL != nil,
	})

	return *user, nil
}

func isLockedName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && !strings.EqualFold(name, defaultUserFirstName)
}

func (s *Service) UploadAvatar(ctx context.Context, actor ActorContext, data []byte, contentType string, ext string) (User, error) {
	// Fetch current profile so we can preserve firstName in the UPDATE.
	current, err := s.repository.FindUserByID(ctx, actor.UserID)
	if err != nil {
		return User{}, err
	}

	key := fmt.Sprintf("%d/%s.%s", actor.UserID, uuid.NewString(), ext)
	fileURL, err := s.storage.PutObject(ctx, platformstorage.BucketProfileImages, key, contentType, data)
	if err != nil {
		return User{}, fmt.Errorf("upload avatar: %w", err)
	}

	now := time.Now()
	updated, err := s.repository.UpdateProfile(ctx, actor.UserID, UpdateProfileRequest{
		FirstName:       current.FirstName,
		LastName:        current.LastName,
		Email:           current.Email,
		Gender:          current.Gender,
		ProfileImageURL: &fileURL,
	}, now)
	if err != nil {
		return User{}, err
	}
	return *updated, nil
}

func (s *Service) GetUser(ctx context.Context, actor ActorContext, userID int64) (User, error) {
	if !canReadUser(actor, userID) {
		return User{}, ErrForbidden
	}

	user, err := s.repository.FindUserByID(ctx, userID)
	if err != nil {
		return User{}, err
	}
	return *user, nil
}

func (s *Service) ListAddresses(ctx context.Context, actor ActorContext, userID int64) ([]Address, error) {
	if !canReadUser(actor, userID) {
		return nil, ErrForbidden
	}
	return s.repository.ListAddresses(ctx, userID)
}

func (s *Service) CreateAddress(ctx context.Context, actor ActorContext, userID int64, input CreateAddressRequest) (Address, error) {
	if !canWriteUser(actor, userID) {
		return Address{}, ErrForbidden
	}
	if err := validateAddress(input); err != nil {
		return Address{}, err
	}
	input = markConfirmedLocation(actor, input)

	address, err := s.repository.CreateAddress(ctx, userID, input)
	if err != nil {
		return Address{}, err
	}

	s.recordChange(ctx, actor, "user_addresses", address.ID, "INSERT", activityCreateAddress, "USER_ADDRESS", address.ID, input)
	return *address, nil
}

func (s *Service) UpdateAddress(ctx context.Context, actor ActorContext, addressID int64, input UpdateAddressRequest) (Address, error) {
	if err := validateAddress(input); err != nil {
		return Address{}, err
	}
	input = markConfirmedLocation(actor, input)

	address, err := s.repository.UpdateAddress(ctx, actor.UserID, addressID, input)
	if err != nil {
		return Address{}, err
	}

	s.recordChange(ctx, actor, "user_addresses", address.ID, "UPDATE", activityUpdateAddress, "USER_ADDRESS", address.ID, input)
	return *address, nil
}

func (s *Service) DeleteAddress(ctx context.Context, actor ActorContext, addressID int64) error {
	if err := s.repository.DeleteAddress(ctx, actor.UserID, addressID); err != nil {
		return err
	}

	s.recordChange(ctx, actor, "user_addresses", addressID, "DELETE", activityDeleteAddress, "USER_ADDRESS", addressID, map[string]any{
		"deleted": true,
	})
	return nil
}

func (s *Service) ListRoles(ctx context.Context, actor ActorContext, userID int64) ([]Role, error) {
	if !canReadUser(actor, userID) {
		return nil, ErrForbidden
	}
	return s.repository.ListRoles(ctx, userID)
}

func (s *Service) ListSessions(ctx context.Context, actor ActorContext, userID int64) ([]Session, error) {
	if !canReadUser(actor, userID) {
		return nil, ErrForbidden
	}
	return s.repository.ListSessions(ctx, userID)
}

// DeleteAccount soft-deletes the caller's own account.
// PII is anonymized, all sessions are revoked, and all role memberships are deactivated.
// Business records (orders, quotations, payments) are preserved for legal compliance.
func (s *Service) DeleteAccount(ctx context.Context, actor ActorContext) error {
	user, err := s.repository.FindUserByID(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if user.Status == "DELETED" {
		return ErrAccountDeleted
	}
	blockers, err := s.repository.GetAccountDeletionBlockers(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if blockers.HasAny() {
		return ErrAccountDeletionBlocked
	}
	if err := s.repository.SoftDeleteAccount(ctx, actor.UserID); err != nil {
		return err
	}
	revocation.Revoke(actor.UserID, 20*time.Minute)
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:      actor.UserID,
		Module:      auditlog.ModuleUsers,
		EntityType:  auditlog.EntityUser,
		EntityID:    actor.UserID,
		Action:      auditlog.ActionDelete,
		Description: "Account self-deleted and PII anonymized",
		NewValue:    map[string]any{"status": "DELETED", "self_initiated": true},
		IPAddress:   actor.IPAddress,
		DeviceInfo:  actor.UserAgent,
	})
	return nil
}

func (s *Service) recordChange(ctx context.Context, actor ActorContext, table string, recordID int64, action string, activityType string, entity string, entityID int64, data any) {
	dataJSON := mustJSON(data)
	_ = s.repository.CreateUserActivity(ctx, CreateActivityInput{
		UserID:   actor.UserID,
		Type:     activityType,
		Entity:   entity,
		EntityID: entityID,
		DataJSON: dataJSON,
		At:       time.Now(),
	})

	auditAction := auditlog.ActionUpdate
	switch action {
	case "INSERT":
		auditAction = auditlog.ActionCreate
	case "DELETE":
		auditAction = auditlog.ActionDelete
	}

	entityType := auditlog.EntityUser
	if entity == "USER_ADDRESS" {
		entityType = auditlog.EntityUserAddress
	}

	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:      actor.UserID,
		Module:      auditlog.ModuleUsers,
		EntityType:  entityType,
		EntityID:    entityID,
		Action:      auditAction,
		Description: activityType,
		NewValue:    data,
		IPAddress:   actor.IPAddress,
		DeviceInfo:  actor.UserAgent,
	})
}

func canReadUser(actor ActorContext, userID int64) bool {
	return actor.UserID == userID || hasRole(actor, "ADMIN")
}

func canWriteUser(actor ActorContext, userID int64) bool {
	return actor.UserID == userID || hasRole(actor, "ADMIN")
}

func hasRole(actor ActorContext, role string) bool {
	for _, item := range actor.Roles {
		if item == role {
			return true
		}
	}
	return false
}

func isAllowedGender(value string) bool {
	switch value {
	case "", "MALE", "FEMALE", "NON_BINARY", "OTHER", "PREFER_NOT_TO_SAY":
		return true
	default:
		return false
	}
}

func validateAddress(input CreateAddressRequest) error {
	if trimString(input.AddressLine1) == "" {
		return ErrInvalidAddress
	}
	if input.Latitude != nil && (*input.Latitude < -90 || *input.Latitude > 90) {
		return ErrInvalidAddress
	}
	if input.Longitude != nil && (*input.Longitude < -180 || *input.Longitude > 180) {
		return ErrInvalidAddress
	}
	if input.GPSAccuracyM != nil && *input.GPSAccuracyM < 0 {
		return ErrInvalidAddress
	}
	if input.LocationSource != nil && !isAllowedLocationSource(*input.LocationSource) {
		return ErrInvalidAddress
	}
	return nil
}

func markConfirmedLocation(actor ActorContext, input CreateAddressRequest) CreateAddressRequest {
	if input.LocationSource != nil && strings.TrimSpace(*input.LocationSource) != "" {
		input.LocationConfirmedBy = &actor.UserID
		now := time.Now()
		input.LocationConfirmedAt = &now
	}
	return input
}

func isAllowedLocationSource(value string) bool {
	switch strings.TrimSpace(value) {
	case "", "gps_confirmed", "nursery_default", "map_selected", "address_search", "admin_updated":
		return true
	default:
		return false
	}
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(data)
}

func trimString(value string) string {
	return strings.TrimSpace(value)
}
