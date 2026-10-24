package users

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/otp"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/revocation"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/email"
	platformstorage "github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
	"github.com/redis/go-redis/v9"
)

type ActorContext = authctx.ActorContext

var (
	ErrForbidden              = apperrs.ErrForbidden
	ErrInvalidInput           = apperrs.ErrInvalidInput
	ErrInvalidAddress         = errors.New("invalid address")
	ErrAccountDeleted         = errors.New("account has already been deleted")
	ErrAccountDeletionBlocked = errors.New("account deletion blocked by active business records")
)

type Service struct {
	repository Repository
	storage    *platformstorage.Client
	auditSvc   *auditlog.Service
	redis      redis.Cmdable
	email      EmailSender
	emailOTPs  *otp.Store
	sms        SMSSender
	mobileOTPs *otp.Store
}

func NewService(repository Repository, storage *platformstorage.Client, auditSvc *auditlog.Service, redisClients ...redis.Cmdable) *Service {
	var rdb redis.Cmdable
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	return &Service{repository: repository, storage: storage, auditSvc: auditSvc, redis: rdb}
}

func (s *Service) Me(ctx context.Context, actor ActorContext) (User, error) {
	return s.GetUser(ctx, actor, actor.UserID)
}

func (s *Service) UpdateMe(ctx context.Context, actor ActorContext, input UpdateProfileRequest) (User, error) {
	input.FirstName = trimString(input.FirstName)
	if input.FirstName == "" {
		return User{}, ErrInvalidInput
	}
	// The photo changes only through POST/DELETE /users/me/avatar, so a
	// profile save keeps it and nobody can point it at an outside URL (it
	// shows on market ads as the seller's selfie).
	input.ProfileImageURL = nil
	// Email: nil leaves it unchanged, "" clears it. Addresses are stored
	// lower-cased so the unique index and verification agree; a changed
	// address loses its verified flag (see UpdateProfile SQL).
	if input.Email != nil && trimString(*input.Email) == "" {
		empty := ""
		input.Email = &empty
	} else if input.Email != nil {
		addr, ok := email.NormalizeAddress(*input.Email)
		if !ok {
			return User{}, ErrInvalidInput
		}
		input.Email = &addr
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

func (s *Service) CompleteOnboarding(ctx context.Context, actor ActorContext, input CompleteOnboardingRequest) (User, error) {
	activity := strings.ToUpper(trimString(input.InitialActivity))
	if !isAllowedInitialActivity(activity) {
		return User{}, ErrInvalidInput
	}

	now := time.Now()
	user, err := s.repository.CompleteOnboarding(ctx, actor.UserID, activity, now)
	if err != nil {
		return User{}, err
	}

	s.recordChange(ctx, actor, "users", user.ID, "UPDATE", activityCompleteOnboarding, "USER", user.ID, map[string]any{
		"initial_activity": activity,
	})

	return *user, nil
}

func isAllowedInitialActivity(activity string) bool {
	switch activity {
	case "CUSTOMER", "OWNER", "DRIVER", "MANAGER":
		return true
	default:
		return false
	}
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

	updated, err := s.setProfileImage(ctx, actor.UserID, fileURL)
	if err != nil {
		return User{}, err
	}
	s.removeOldPhoto(ctx, current.ProfileImageURL)
	return updated, nil
}

// DeleteAvatar removes the profile photo (the seller selfie) and its file.
// Without it the user cannot post market ads or start market chats.
func (s *Service) DeleteAvatar(ctx context.Context, actor ActorContext) (User, error) {
	current, err := s.repository.FindUserByID(ctx, actor.UserID)
	if err != nil {
		return User{}, err
	}
	updated, err := s.setProfileImage(ctx, actor.UserID, "")
	if err != nil {
		return User{}, err
	}
	s.removeOldPhoto(ctx, current.ProfileImageURL)
	return updated, nil
}

// setProfileImage changes only the photo; "" clears it.
func (s *Service) setProfileImage(ctx context.Context, userID int64, url string) (User, error) {
	current, err := s.repository.FindUserByID(ctx, userID)
	if err != nil {
		return User{}, err
	}
	updated, err := s.repository.UpdateProfile(ctx, userID, UpdateProfileRequest{
		FirstName:       current.FirstName,
		LastName:        current.LastName,
		Gender:          current.Gender,
		ProfileImageURL: &url,
	}, time.Now())
	if err != nil {
		return User{}, err
	}
	return *updated, nil
}

// removeOldPhoto deletes a replaced or removed selfie from storage. Best
// effort: a leftover file is not worth failing the user's request.
func (s *Service) removeOldPhoto(ctx context.Context, url *string) {
	if s.storage == nil || url == nil || *url == "" {
		return
	}
	if err := s.storage.RemoveByURL(ctx, *url); err != nil {
		slog.WarnContext(ctx, "remove old profile photo", "err", err)
	}
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
	s.blocklistActorToken(ctx, actor)
	if s.auditSvc == nil {
		return nil
	}
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

func (s *Service) blocklistActorToken(ctx context.Context, actor ActorContext) {
	if actor.TokenJTI == "" || actor.TokenExpEpoch == 0 {
		return
	}
	redisutil.BlocklistToken(ctx, s.redis, slog.Default(), actor.TokenJTI, time.Until(time.Unix(actor.TokenExpEpoch, 0)))
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

	if s.auditSvc == nil {
		return
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
	return actor.UserID == userID || actor.HasRole("ADMIN")
}

func canWriteUser(actor ActorContext, userID int64) bool {
	return actor.UserID == userID || actor.HasRole("ADMIN")
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
