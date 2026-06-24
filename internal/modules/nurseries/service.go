package nurseries

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

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(ctx context.Context, input ListNurseriesRequest) ([]Nursery, Pagination, error) {
	input = normalizeList(input)
	nurseries, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	return nurseries, Pagination{
		Page:       input.Page,
		PerPage:    input.PerPage,
		Total:      total,
		TotalPages: totalPages(total, input.PerPage),
	}, nil
}

func (s *Service) ListMine(ctx context.Context, userID int64) ([]Nursery, error) {
	return s.repository.ListByUserID(ctx, userID)
}

func (s *Service) Get(ctx context.Context, nurseryID int64) (Nursery, error) {
	nursery, err := s.repository.FindByID(ctx, nurseryID)
	if err != nil {
		return Nursery{}, err
	}
	return *nursery, nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, input CreateNurseryRequest) (Nursery, error) {
	if !canManageNurseries(actor) {
		return Nursery{}, ErrForbidden
	}
	input = normalizeNursery(input)
	if err := validateNursery(input); err != nil {
		return Nursery{}, err
	}
	nursery, err := s.repository.Create(ctx, actor.UserID, input)
	if err != nil {
		return Nursery{}, err
	}
	s.audit(ctx, actor, "nurseries", nursery.ID, actionInsert, input)
	return *nursery, nil
}

func (s *Service) Update(ctx context.Context, actor ActorContext, nurseryID int64, input UpdateNurseryRequest) (Nursery, error) {
	if !canManageNurseries(actor) {
		return Nursery{}, ErrForbidden
	}
	input = normalizeNursery(input)
	if err := validateNursery(input); err != nil {
		return Nursery{}, err
	}
	nursery, err := s.repository.Update(ctx, actor.UserID, nurseryID, input)
	if err != nil {
		return Nursery{}, err
	}
	s.audit(ctx, actor, "nurseries", nursery.ID, actionUpdate, input)
	return *nursery, nil
}

func (s *Service) Delete(ctx context.Context, actor ActorContext, nurseryID int64) error {
	if !canManageNurseries(actor) {
		return ErrForbidden
	}
	if err := s.repository.Delete(ctx, actor.UserID, nurseryID); err != nil {
		return err
	}
	s.audit(ctx, actor, "nurseries", nurseryID, actionDelete, map[string]any{"status": "DELETED"})
	return nil
}

func (s *Service) ListAddresses(ctx context.Context, actor ActorContext, nurseryID int64) ([]Address, error) {
	if !canManageNurseries(actor) {
		return nil, ErrForbidden
	}
	if _, err := s.repository.FindByID(ctx, nurseryID); err != nil {
		return nil, err
	}
	return s.repository.ListAddresses(ctx, nurseryID)
}

func (s *Service) CreateAddress(ctx context.Context, actor ActorContext, nurseryID int64, input AddressRequest) (Address, error) {
	if !canManageNurseries(actor) {
		return Address{}, ErrForbidden
	}
	if _, err := s.repository.FindByID(ctx, nurseryID); err != nil {
		return Address{}, err
	}
	if err := validateAddress(input); err != nil {
		return Address{}, err
	}
	address, err := s.repository.CreateAddress(ctx, nurseryID, input)
	if err != nil {
		return Address{}, err
	}
	s.audit(ctx, actor, "nursery_addresses", address.ID, actionInsert, input)
	return *address, nil
}

func (s *Service) UpdateAddress(ctx context.Context, actor ActorContext, addressID int64, input AddressRequest) (Address, error) {
	if !canManageNurseries(actor) {
		return Address{}, ErrForbidden
	}
	if err := validateAddress(input); err != nil {
		return Address{}, err
	}
	address, err := s.repository.UpdateAddress(ctx, addressID, input)
	if err != nil {
		return Address{}, err
	}
	s.audit(ctx, actor, "nursery_addresses", address.ID, actionUpdate, input)
	return *address, nil
}

func (s *Service) DeleteAddress(ctx context.Context, actor ActorContext, addressID int64) error {
	if !canManageNurseries(actor) {
		return ErrForbidden
	}
	if err := s.repository.DeleteAddress(ctx, addressID); err != nil {
		return err
	}
	s.audit(ctx, actor, "nursery_addresses", addressID, actionDelete, map[string]any{"deleted": true})
	return nil
}

func (s *Service) ListUsers(ctx context.Context, actor ActorContext, nurseryID int64) ([]UserLink, error) {
	if !canManageNurseries(actor) {
		return nil, ErrForbidden
	}
	if _, err := s.repository.FindByID(ctx, nurseryID); err != nil {
		return nil, err
	}
	return s.repository.ListUsers(ctx, nurseryID)
}

func (s *Service) AddUser(ctx context.Context, actor ActorContext, nurseryID int64, input AddUserRequest) (UserLink, error) {
	if !canManageNurseries(actor) {
		return UserLink{}, ErrForbidden
	}
	if input.UserID <= 0 {
		return UserLink{}, ErrInvalidInput
	}
	if _, err := s.repository.FindByID(ctx, nurseryID); err != nil {
		return UserLink{}, err
	}
	user, err := s.repository.AddUser(ctx, nurseryID, input)
	if err != nil {
		return UserLink{}, err
	}
	s.audit(ctx, actor, "nursery_users", user.ID, actionInsert, input)
	return *user, nil
}

func (s *Service) RemoveUser(ctx context.Context, actor ActorContext, nurseryID int64, userID int64) error {
	if !canManageNurseries(actor) {
		return ErrForbidden
	}
	if err := s.repository.RemoveUser(ctx, nurseryID, userID); err != nil {
		return err
	}
	s.audit(ctx, actor, "nursery_users", userID, actionDelete, map[string]any{"nursery_id": nurseryID, "user_id": userID})
	return nil
}

func (s *Service) audit(ctx context.Context, actor ActorContext, table string, recordID int64, action string, data any) {
	_ = s.repository.CreateAuditLog(ctx, CreateAuditInput{
		TableName: table,
		RecordID:  recordID,
		Action:    action,
		ChangedBy: actor.UserID,
		SourceIP:  actor.IPAddress,
		UserAgent: actor.UserAgent,
		NewJSON:   mustJSON(data),
		At:        time.Now(),
	})
}

func normalizeList(input ListNurseriesRequest) ListNurseriesRequest {
	if input.Page <= 0 {
		input.Page = 1
	}
	if input.PerPage <= 0 {
		input.PerPage = 20
	}
	if input.PerPage > 100 {
		input.PerPage = 100
	}
	input.Search = strings.TrimSpace(input.Search)
	input.City = strings.TrimSpace(input.City)
	input.State = strings.TrimSpace(input.State)
	input.NurseryStatus = strings.ToUpper(strings.TrimSpace(input.NurseryStatus))
	input.VerificationStatus = strings.ToUpper(strings.TrimSpace(input.VerificationStatus))
	return input
}

func normalizeNursery(input CreateNurseryRequest) CreateNurseryRequest {
	input.Name = strings.TrimSpace(input.Name)
	upperOptional(input.Status)
	return input
}

func validateNursery(input CreateNurseryRequest) error {
	if input.Name == "" {
		return ErrInvalidInput
	}
	status := statusOrActive(input.Status)
	switch status {
	case "ACTIVE", "INACTIVE", "SUSPENDED", "DELETED":
		return nil
	default:
		return ErrInvalidInput
	}
}

func validateAddress(input AddressRequest) error {
	if input.AddressLine1 != nil && strings.TrimSpace(*input.AddressLine1) == "" {
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

func canManageNurseries(actor ActorContext) bool {
	return hasRole(actor, "ADMIN") || hasRole(actor, "NURSERY_OWNER")
}

func hasRole(actor ActorContext, role string) bool {
	for _, item := range actor.Roles {
		if item == role {
			return true
		}
	}
	return false
}

func totalPages(total int64, perPage int) int {
	if total == 0 {
		return 0
	}
	return int((total + int64(perPage) - 1) / int64(perPage))
}

func upperOptional(value *string) {
	if value == nil {
		return
	}
	*value = strings.ToUpper(strings.TrimSpace(*value))
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(data)
}
