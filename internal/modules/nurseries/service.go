package nurseries

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
)

var (
	ErrForbidden               = errors.New("forbidden")
	ErrInvalidInput            = errors.New("invalid input")
	ErrInvalidAddress          = errors.New("invalid address")
	ErrAlreadyOwner           = errors.New("user already owns a nursery")
	ErrNotNurseryOwner        = errors.New("only the nursery owner can perform this action")
	ErrManagerCannotOwnNursery = errors.New("managers cannot register a nursery")
	ErrDriverCannotOwnNursery  = errors.New("approved drivers cannot register a nursery")
)

// TrialCreator is satisfied by the subscriptions.Service to avoid a circular import.
type TrialCreator interface {
	CreateTrialForOwner(ctx context.Context, ownerUserID int64, approvalDate time.Time) error
}

type Service struct {
	repository Repository
	trialSvc   TrialCreator // may be nil
	auditSvc   *auditlog.Service
}

func NewService(repository Repository, auditSvc *auditlog.Service) *Service {
	return &Service{repository: repository, auditSvc: auditSvc}
}

func NewServiceWithTrial(repository Repository, trialSvc TrialCreator, auditSvc *auditlog.Service) *Service {
	return &Service{repository: repository, trialSvc: trialSvc, auditSvc: auditSvc}
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

// ListMine returns nurseries where the user is a manager.
func (s *Service) ListMine(ctx context.Context, userID int64) ([]Nursery, error) {
	return s.repository.ListByUserID(ctx, userID)
}

// GetOwned returns the nursery owned by the user.
func (s *Service) GetOwned(ctx context.Context, userID int64) (Nursery, error) {
	nursery, err := s.repository.FindOwnedByUser(ctx, userID)
	if err != nil {
		return Nursery{}, err
	}
	return *nursery, nil
}

// ListManagers returns all managers for a nursery. Owner or admin only.
func (s *Service) ListManagers(ctx context.Context, actor ActorContext, nurseryID int64) ([]UserLink, error) {
	isOwner, _ := s.repository.IsNurseryOwner(ctx, nurseryID, actor.UserID)
	if !isOwner && !hasRole(actor, "ADMIN") && !hasRole(actor, "SUPER_ADMIN") {
		return nil, ErrForbidden
	}
	return s.repository.ListManagers(ctx, nurseryID)
}

// AddManager adds a manager to a nursery. Owner only.
func (s *Service) AddManager(ctx context.Context, actor ActorContext, nurseryID int64, input AddManagerRequest) (UserLink, error) {
	isOwner, _ := s.repository.IsNurseryOwner(ctx, nurseryID, actor.UserID)
	if !isOwner && !hasRole(actor, "ADMIN") && !hasRole(actor, "SUPER_ADMIN") {
		return UserLink{}, ErrNotNurseryOwner
	}
	if input.UserID <= 0 {
		return UserLink{}, ErrInvalidInput
	}
	manager, err := s.repository.AddManager(ctx, nurseryID, actor.UserID, input)
	if err != nil {
		return UserLink{}, err
	}
	s.audit(ctx, actor, auditlog.EntityNurseryUser, manager.ID, auditlog.ActionCreate, "Manager added", nil, input)
	return *manager, nil
}

// ConnectDriver connects a driver to a nursery.
func (s *Service) ConnectDriver(ctx context.Context, actor ActorContext, nurseryID int64, driverUserID int64) (NurseryDriver, error) {
	isOwner, _ := s.repository.IsNurseryOwner(ctx, nurseryID, actor.UserID)
	isMember, _ := s.repository.IsNurseryMember(ctx, nurseryID, actor.UserID)
	if !isOwner && !isMember && !hasRole(actor, "ADMIN") {
		return NurseryDriver{}, ErrForbidden
	}
	nd, err := s.repository.ConnectDriver(ctx, nurseryID, driverUserID, actor.UserID)
	if err != nil {
		return NurseryDriver{}, err
	}
	return *nd, nil
}

// ApproveDriverConnection approves a driver connection. Owner only.
func (s *Service) ApproveDriverConnection(ctx context.Context, actor ActorContext, nurseryID int64, driverUserID int64) error {
	isOwner, _ := s.repository.IsNurseryOwner(ctx, nurseryID, actor.UserID)
	if !isOwner && !hasRole(actor, "ADMIN") {
		return ErrNotNurseryOwner
	}
	return s.repository.ApproveDriverConnection(ctx, nurseryID, driverUserID, actor.UserID)
}

// ListConnectedDrivers returns all drivers connected to a nursery.
func (s *Service) ListConnectedDrivers(ctx context.Context, actor ActorContext, nurseryID int64) ([]NurseryDriver, error) {
	isOwner, _ := s.repository.IsNurseryOwner(ctx, nurseryID, actor.UserID)
	isMember, _ := s.repository.IsNurseryMember(ctx, nurseryID, actor.UserID)
	if !isOwner && !isMember && !hasRole(actor, "ADMIN") {
		return nil, ErrForbidden
	}
	return s.repository.ListConnectedDrivers(ctx, nurseryID)
}

func (s *Service) Get(ctx context.Context, nurseryID int64) (Nursery, error) {
	nursery, err := s.repository.FindByID(ctx, nurseryID)
	if err != nil {
		return Nursery{}, err
	}
	return *nursery, nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, input CreateNurseryRequest) (Nursery, error) {
	// Any authenticated user can register a nursery (they become the owner).
	// Admins bypass the one-per-user rules below.
	// V1 rules: one nursery per user; managers cannot own a nursery.
	if !hasRole(actor, "ADMIN") && !hasRole(actor, "SUPER_ADMIN") {
		alreadyOwns, err := s.repository.UserOwnsANursery(ctx, actor.UserID)
		if err != nil {
			return Nursery{}, err
		}
		if alreadyOwns {
			return Nursery{}, ErrAlreadyOwner
		}
		isManager, err := s.repository.UserIsManager(ctx, actor.UserID)
		if err != nil {
			return Nursery{}, err
		}
		if isManager {
			return Nursery{}, ErrManagerCannotOwnNursery
		}
		isDriver, err := s.repository.UserIsApprovedDriver(ctx, actor.UserID)
		if err != nil {
			return Nursery{}, err
		}
		if isDriver {
			return Nursery{}, ErrDriverCannotOwnNursery
		}
	}
	input = normalizeNursery(input)
	if err := validateNursery(input); err != nil {
		return Nursery{}, err
	}
	// Non-admin actors own the nursery they create; admins leave owner_user_id nil unless specified.
	if !hasRole(actor, "ADMIN") && !hasRole(actor, "SUPER_ADMIN") {
		input.OwnerUserID = &actor.UserID
	}
	nursery, err := s.repository.Create(ctx, actor.UserID, input)
	if err != nil {
		return Nursery{}, err
	}
	s.audit(ctx, actor, auditlog.EntityNursery, nursery.ID, auditlog.ActionCreate,
		fmt.Sprintf("Nursery %q registered", nursery.Name), nil, input)
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
	old, _ := s.repository.FindByID(ctx, nurseryID)
	nursery, err := s.repository.Update(ctx, actor.UserID, nurseryID, input)
	if err != nil {
		return Nursery{}, err
	}
	s.audit(ctx, actor, auditlog.EntityNursery, nursery.ID, auditlog.ActionUpdate,
		fmt.Sprintf("Nursery %q updated", nursery.Name), old, input)
	return *nursery, nil
}

func (s *Service) UpdateStatus(ctx context.Context, actor ActorContext, nurseryID int64, status string) (Nursery, error) {
	if !hasRole(actor, "ADMIN") && !hasRole(actor, "SUPER_ADMIN") {
		return Nursery{}, ErrForbidden
	}
	status = strings.ToUpper(strings.TrimSpace(status))
	if status == "" {
		return Nursery{}, ErrInvalidInput
	}
	old, _ := s.repository.FindByID(ctx, nurseryID)
	nursery, err := s.repository.UpdateStatusOnly(ctx, actor.UserID, nurseryID, status)
	if err != nil {
		return Nursery{}, err
	}
	oldStatus := ""
	if old != nil {
		oldStatus = old.Status
	}
	s.audit(ctx, actor, auditlog.EntityNursery, nursery.ID, auditlog.ActionUpdate,
		fmt.Sprintf("Nursery status %s → %s", oldStatus, status),
		map[string]any{"status": oldStatus},
		map[string]any{"status": status})

	// Auto-create 6-month TRIAL subscription when admin approves a nursery.
	if status == "APPROVED" && s.trialSvc != nil && nursery.OwnerUserID != nil {
		_ = s.trialSvc.CreateTrialForOwner(ctx, *nursery.OwnerUserID, time.Now())
	}

	return *nursery, nil
}

func (s *Service) Delete(ctx context.Context, actor ActorContext, nurseryID int64) error {
	if !canManageNurseries(actor) {
		return ErrForbidden
	}
	old, _ := s.repository.FindByID(ctx, nurseryID)
	if err := s.repository.Delete(ctx, actor.UserID, nurseryID); err != nil {
		return err
	}
	name := ""
	if old != nil {
		name = old.Name
	}
	s.audit(ctx, actor, auditlog.EntityNursery, nurseryID, auditlog.ActionDelete,
		fmt.Sprintf("Nursery %q deleted", name), old, map[string]any{"status": "DELETED"})
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
	s.audit(ctx, actor, auditlog.EntityNurseryAddr, address.ID, auditlog.ActionCreate, "Nursery address added", nil, input)
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
	s.audit(ctx, actor, auditlog.EntityNurseryAddr, address.ID, auditlog.ActionUpdate, "Nursery address updated", nil, input)
	return *address, nil
}

func (s *Service) DeleteAddress(ctx context.Context, actor ActorContext, addressID int64) error {
	if !canManageNurseries(actor) {
		return ErrForbidden
	}
	if err := s.repository.DeleteAddress(ctx, addressID); err != nil {
		return err
	}
	s.audit(ctx, actor, auditlog.EntityNurseryAddr, addressID, auditlog.ActionDelete, "Nursery address removed", nil, map[string]any{"deleted": true})
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
	s.audit(ctx, actor, auditlog.EntityNurseryUser, user.ID, auditlog.ActionCreate, "Nursery user added", nil, input)
	return *user, nil
}

// GetCustomers returns buyers who accepted a CUSTOMER_INVITE for a nursery.
// ADMIN/SUPER_ADMIN see all; NURSERY_OWNER must own it; MANAGER must be a member.
func (s *Service) GetCustomers(ctx context.Context, actor ActorContext, nurseryID int64) ([]Customer, error) {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return s.repository.GetCustomers(ctx, nurseryID)
	}
	isOwner, _ := s.repository.IsNurseryOwner(ctx, nurseryID, actor.UserID)
	if isOwner {
		return s.repository.GetCustomers(ctx, nurseryID)
	}
	isMember, _ := s.repository.IsNurseryMember(ctx, nurseryID, actor.UserID)
	if isMember {
		return s.repository.GetCustomers(ctx, nurseryID)
	}
	return nil, ErrForbidden
}

func (s *Service) RemoveUser(ctx context.Context, actor ActorContext, nurseryID int64, userID int64) error {
	if !canManageNurseries(actor) {
		return ErrForbidden
	}
	if err := s.repository.RemoveUser(ctx, nurseryID, userID); err != nil {
		return err
	}
	s.audit(ctx, actor, auditlog.EntityNurseryUser, userID, auditlog.ActionDelete,
		"Nursery user removed", nil, map[string]any{"nursery_id": nurseryID, "user_id": userID})
	return nil
}

func (s *Service) audit(ctx context.Context, actor ActorContext, entityType string, entityID int64, action auditlog.Action, description string, oldValue, newValue any) {
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:      actor.UserID,
		Module:      auditlog.ModuleNurseries,
		EntityType:  entityType,
		EntityID:    entityID,
		Action:      action,
		Description: description,
		OldValue:    oldValue,
		NewValue:    newValue,
		IPAddress:   actor.IPAddress,
		DeviceInfo:  actor.UserAgent,
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
	case "ACTIVE", "INACTIVE", "SUSPENDED", "DELETED", "PENDING", "APPROVED", "REJECTED":
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
	return hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") || hasRole(actor, "NURSERY_OWNER")
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

