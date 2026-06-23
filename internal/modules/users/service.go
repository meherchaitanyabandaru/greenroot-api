package users

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrForbidden      = errors.New("forbidden")
	ErrInvalidInput   = errors.New("invalid input")
	ErrInvalidAddress = errors.New("invalid address")
)

type Service struct {
	repository Repository
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
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

func (s *Service) recordChange(ctx context.Context, actor ActorContext, table string, recordID int64, action string, activityType string, entity string, entityID int64, data any) {
	now := time.Now()
	dataJSON := mustJSON(data)
	_ = s.repository.CreateUserActivity(ctx, CreateActivityInput{
		UserID:   actor.UserID,
		Type:     activityType,
		Entity:   entity,
		EntityID: entityID,
		DataJSON: dataJSON,
		At:       now,
	})
	_ = s.repository.CreateAuditLog(ctx, CreateAuditInput{
		TableName: table,
		RecordID:  recordID,
		Action:    action,
		ChangedBy: actor.UserID,
		SourceIP:  actor.IPAddress,
		UserAgent: actor.UserAgent,
		NewJSON:   dataJSON,
		At:        now,
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
	return nil
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
