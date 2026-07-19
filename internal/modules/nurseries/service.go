package nurseries

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
	"github.com/redis/go-redis/v9"
)

var (
	ErrForbidden               = apperrs.ErrForbidden
	ErrInvalidInput            = apperrs.ErrInvalidInput
	ErrInvalidAddress          = errors.New("invalid address")
	ErrAlreadyOwner            = errors.New("user already owns a nursery")
	ErrNotNurseryOwner         = errors.New("only the nursery owner can perform this action")
	ErrManagerCannotOwnNursery = errors.New("managers cannot register a nursery")
	ErrDriverCannotOwnNursery  = errors.New("approved drivers cannot register a nursery")
	ErrNotMember               = errors.New("user is not an active member of this nursery")
	ErrOwnerCannotLeave        = errors.New("nursery owner cannot leave their own nursery")
)

// TrialCreator is satisfied by the subscriptions.Service to avoid a circular import.
type TrialCreator interface {
	CreateTrialForOwner(ctx context.Context, ownerUserID int64, approvalDate time.Time) error
}

type Service struct {
	repository Repository
	trialSvc   TrialCreator // may be nil
	auditSvc   *auditlog.Service
	redis      redis.Cmdable
}

func NewService(repository Repository, auditSvc *auditlog.Service, redisClients ...redis.Cmdable) *Service {
	return NewServiceWithTrial(repository, nil, auditSvc, redisClients...)
}

func NewServiceWithTrial(repository Repository, trialSvc TrialCreator, auditSvc *auditlog.Service, redisClients ...redis.Cmdable) *Service {
	var rdb redis.Cmdable
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	return &Service{repository: repository, trialSvc: trialSvc, auditSvc: auditSvc, redis: rdb}
}

func (s *Service) List(ctx context.Context, input ListNurseriesRequest) ([]Nursery, Pagination, error) {
	input = normalizeList(input)
	nurseries, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	for i := range nurseries {
		nurseries[i] = enrichNursery(NurseryActor{}, nurseries[i])
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
	nurseries, err := s.repository.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range nurseries {
		nurseries[i] = enrichNursery(NurseryActor{UserID: userID}, nurseries[i])
	}
	return nurseries, nil
}

// GetOwned returns the nursery owned by the user.
func (s *Service) GetOwned(ctx context.Context, userID int64) (Nursery, error) {
	nursery, err := s.repository.FindOwnedByUser(ctx, userID)
	if err != nil {
		return Nursery{}, err
	}
	return enrichNursery(NurseryActor{UserID: userID}, *nursery), nil
}

// ListManagers returns all managers for a nursery. Owner or admin only.
func (s *Service) ListManagers(ctx context.Context, actor ActorContext, nurseryID int64) ([]UserLink, error) {
	isOwner, _ := s.repository.IsNurseryOwner(ctx, nurseryID, actor.UserID)
	if !isOwner && !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		return nil, ErrForbidden
	}
	return s.repository.ListManagers(ctx, nurseryID)
}

// AddManager adds a manager to a nursery. Owner only.
func (s *Service) AddManager(ctx context.Context, actor ActorContext, nurseryID int64, input AddManagerRequest) (UserLink, error) {
	isOwner, _ := s.repository.IsNurseryOwner(ctx, nurseryID, actor.UserID)
	if !isOwner && !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
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
	s.invalidateWorkspaceUsers(ctx, actor.UserID, input.UserID)
	return *manager, nil
}

// ConnectDriver connects a driver to a nursery.
func (s *Service) ConnectDriver(ctx context.Context, actor ActorContext, nurseryID int64, driverUserID int64) (NurseryDriver, error) {
	isOwner, _ := s.repository.IsNurseryOwner(ctx, nurseryID, actor.UserID)
	isMember, _ := s.repository.IsNurseryMember(ctx, nurseryID, actor.UserID)
	if !isOwner && !isMember && !actor.HasRole("ADMIN") {
		return NurseryDriver{}, ErrForbidden
	}
	nd, err := s.repository.ConnectDriver(ctx, nurseryID, driverUserID, actor.UserID)
	if err != nil {
		return NurseryDriver{}, err
	}
	s.invalidateWorkspaceUsers(ctx, actor.UserID, driverUserID)
	return *nd, nil
}

// ApproveDriverConnection approves a driver connection. Owner only.
func (s *Service) ApproveDriverConnection(ctx context.Context, actor ActorContext, nurseryID int64, driverUserID int64) error {
	isOwner, _ := s.repository.IsNurseryOwner(ctx, nurseryID, actor.UserID)
	if !isOwner && !actor.HasRole("ADMIN") {
		return ErrNotNurseryOwner
	}
	if err := s.repository.ApproveDriverConnection(ctx, nurseryID, driverUserID, actor.UserID); err != nil {
		return err
	}
	s.invalidateWorkspaceUsers(ctx, actor.UserID, driverUserID)
	return nil
}

// ListConnectedDrivers returns all drivers connected to a nursery. Owner or admin only.
func (s *Service) ListConnectedDrivers(ctx context.Context, actor ActorContext, nurseryID int64) ([]NurseryDriver, error) {
	isOwner, _ := s.repository.IsNurseryOwner(ctx, nurseryID, actor.UserID)
	if !isOwner && !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		return nil, ErrForbidden
	}
	return s.repository.ListConnectedDrivers(ctx, nurseryID)
}

func (s *Service) Get(ctx context.Context, nurseryID int64) (Nursery, error) {
	nursery, err := s.repository.FindByID(ctx, nurseryID)
	if err != nil {
		return Nursery{}, err
	}
	return enrichNursery(NurseryActor{}, *nursery), nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, input CreateNurseryRequest) (Nursery, error) {
	// Any authenticated user can register a nursery (they become the owner).
	// Admins bypass the one-per-user rules below.
	// V1 rules: one nursery per user; managers cannot own a nursery.
	if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
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
	// Non-admin actors own the nursery they create; admins leave owner_user_id nil unless specified.
	if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		input.OwnerUserID = &actor.UserID
		status := "PENDING"
		input.Status = &status
	}
	input = normalizeNursery(input)
	if err := validateNursery(input); err != nil {
		return Nursery{}, err
	}
	nursery, err := s.repository.Create(ctx, actor.UserID, input)
	if err != nil {
		return Nursery{}, err
	}
	// Auto-create PRIMARY nursery address from registration data when provided.
	if input.AddressLine1 != nil || input.City != nil {
		country := "India"
		if input.Country != nil && *input.Country != "" {
			country = *input.Country
		}
		addrType := "PRIMARY"
		_, addrErr := s.repository.CreateAddress(ctx, nursery.ID, AddressRequest{
			AddressType:  &addrType,
			AddressLine1: input.AddressLine1,
			AddressLine2: input.AddressLine2,
			City:         input.City,
			State:        input.State,
			Country:      &country,
			PostalCode:   input.PostalCode,
			Landmark:     input.Landmark,
			Latitude:     input.Latitude,
			Longitude:    input.Longitude,
			IsPrimary:    true,
		})
		if addrErr != nil {
			s.audit(ctx, actor, auditlog.EntityNurseryAddr, nursery.ID, auditlog.ActionCreate,
				"address auto-create failed on nursery registration", nil, addrErr.Error())
		} else if refreshed, rErr := s.repository.FindByID(ctx, nursery.ID); rErr == nil {
			nursery = refreshed
		}
	}
	s.audit(ctx, actor, auditlog.EntityNursery, nursery.ID, auditlog.ActionCreate,
		fmt.Sprintf("Nursery %q registered", nursery.Name), nil, input)
	s.invalidateWorkspaceUsers(ctx, actor.UserID)
	return enrichNursery(actorFromContext(actor), *nursery), nil
}

func (s *Service) Update(ctx context.Context, actor ActorContext, nurseryID int64, input UpdateNurseryRequest) (Nursery, error) {
	if !canManageNurseries(actor) {
		return Nursery{}, ErrForbidden
	}
	input = normalizeUpdateNursery(input)
	if err := validateUpdateNursery(input); err != nil {
		return Nursery{}, err
	}
	if err := validateBranding(input); err != nil {
		return Nursery{}, err
	}
	old, _ := s.repository.FindByID(ctx, nurseryID)
	nursery, err := s.repository.Update(ctx, actor.UserID, nurseryID, input)
	if err != nil {
		return Nursery{}, err
	}
	s.audit(ctx, actor, auditlog.EntityNursery, nursery.ID, auditlog.ActionUpdate,
		fmt.Sprintf("Nursery %q updated", nursery.Name), old, input)
	s.invalidateNurseryWorkspaceUsers(ctx, nurseryID, actor.UserID)
	return enrichNursery(actorFromContext(actor), *nursery), nil
}

func (s *Service) UpdateStatus(ctx context.Context, actor ActorContext, nurseryID int64, status string) (Nursery, error) {
	if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
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
	if status == "APPROVED" && nursery.OwnerUserID != nil {
		if s.trialSvc != nil {
			_ = s.trialSvc.CreateTrialForOwner(ctx, *nursery.OwnerUserID, time.Now())
		}
		// Upgrade owner's system role from Buyer → Nursery Owner so their next
		// JWT token carries NURSERY_OWNER and market/order access is unlocked.
		_ = s.repository.GrantOwnerRole(ctx, *nursery.OwnerUserID, actor.UserID)
	}

	s.invalidateNurseryWorkspaceUsers(ctx, nurseryID, actor.UserID)
	return enrichNursery(actorFromContext(actor), *nursery), nil
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
	s.invalidateNurseryWorkspaceUsers(ctx, nurseryID, actor.UserID)
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
	input = markConfirmedLocation(actor, input)
	address, err := s.repository.CreateAddress(ctx, nurseryID, input)
	if err != nil {
		return Address{}, err
	}
	s.audit(ctx, actor, auditlog.EntityNurseryAddr, address.ID, auditlog.ActionCreate, "Nursery address added", nil, input)
	s.invalidateNurseryWorkspaceUsers(ctx, nurseryID, actor.UserID)
	return *address, nil
}

func (s *Service) UpdateAddress(ctx context.Context, actor ActorContext, addressID int64, input AddressRequest) (Address, error) {
	if !canManageNurseries(actor) {
		return Address{}, ErrForbidden
	}
	if err := validateAddress(input); err != nil {
		return Address{}, err
	}
	input = markConfirmedLocation(actor, input)
	address, err := s.repository.UpdateAddress(ctx, addressID, input)
	if err != nil {
		return Address{}, err
	}
	s.audit(ctx, actor, auditlog.EntityNurseryAddr, address.ID, auditlog.ActionUpdate, "Nursery address updated", nil, input)
	s.invalidateWorkspaceUsers(ctx, actor.UserID)
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
	s.invalidateWorkspaceUsers(ctx, actor.UserID)
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
	s.invalidateWorkspaceUsers(ctx, actor.UserID, input.UserID)
	return *user, nil
}

// GetCustomers returns buyers who accepted a CUSTOMER_INVITE for a nursery.
// ADMIN/SUPER_ADMIN see all; NURSERY_OWNER must own it; MANAGER must be a member.
func (s *Service) GetCustomers(ctx context.Context, actor ActorContext, nurseryID int64) ([]Customer, error) {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
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
	isSelf := userID == actor.UserID
	isOwner, _ := s.repository.IsNurseryOwner(ctx, nurseryID, actor.UserID)

	// Owner cannot leave their own nursery through this route
	if isSelf && isOwner {
		return ErrOwnerCannotLeave
	}
	// Non-self removal requires owner or admin
	if !isSelf && !isOwner && !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		return ErrForbidden
	}

	if err := s.repository.RemoveUser(ctx, nurseryID, userID); err != nil {
		return err
	}
	// Cancel any PENDING invites for this user in this nursery so they cannot
	// re-enter via an outstanding invite link after being removed.
	_ = s.repository.CancelPendingInvitesForUser(ctx, nurseryID, userID)
	// Invalidate the removed user's sessions so their next request forces re-login
	_ = s.repository.InvalidateUserSessions(ctx, userID)

	s.audit(ctx, actor, auditlog.EntityNurseryUser, userID, auditlog.ActionDelete,
		fmt.Sprintf("Nursery user %d removed from nursery %d", userID, nurseryID),
		nil, map[string]any{"nursery_id": nurseryID, "user_id": userID, "self": isSelf})
	s.invalidateWorkspaceUsers(ctx, actor.UserID, userID)
	return nil
}

// LeaveNursery allows a manager or driver to remove themselves from any nursery they belong to.
// The actor's own nursery membership is found automatically.
func (s *Service) LeaveNursery(ctx context.Context, actor ActorContext) error {
	nurseryID, err := s.repository.FindActiveManagerNursery(ctx, actor.UserID)
	if err != nil {
		return ErrNotMember
	}
	isOwner, _ := s.repository.IsNurseryOwner(ctx, nurseryID, actor.UserID)
	if isOwner {
		return ErrOwnerCannotLeave
	}
	return s.RemoveUser(ctx, actor, nurseryID, actor.UserID)
}

// DisconnectDriver removes a driver from a nursery. Owner only.
func (s *Service) DisconnectDriver(ctx context.Context, actor ActorContext, nurseryID int64, driverUserID int64) error {
	isSelf := driverUserID == actor.UserID
	if !isSelf {
		isOwner, _ := s.repository.IsNurseryOwner(ctx, nurseryID, actor.UserID)
		if !isOwner && !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
			return ErrForbidden
		}
	}
	if err := s.repository.DisconnectDriver(ctx, nurseryID, driverUserID, actor.UserID); err != nil {
		return err
	}
	_ = s.repository.InvalidateUserSessions(ctx, driverUserID)
	s.audit(ctx, actor, auditlog.EntityNurseryUser, driverUserID, auditlog.ActionDelete,
		fmt.Sprintf("Driver %d disconnected from nursery %d", driverUserID, nurseryID),
		nil, map[string]any{"nursery_id": nurseryID, "driver_user_id": driverUserID})
	s.invalidateWorkspaceUsers(ctx, actor.UserID, driverUserID)
	return nil
}

func (s *Service) invalidateNurseryWorkspaceUsers(ctx context.Context, nurseryID int64, fallbackUserIDs ...int64) {
	userIDs, err := s.repository.WorkspaceUserIDs(ctx, nurseryID)
	if err != nil {
		slog.Warn("workspace invalidation user lookup failed", "nursery_id", nurseryID, "error", err)
		userIDs = fallbackUserIDs
	} else {
		userIDs = append(userIDs, fallbackUserIDs...)
	}
	s.invalidateWorkspaceUsers(ctx, userIDs...)
}

func (s *Service) invalidateWorkspaceUsers(ctx context.Context, userIDs ...int64) {
	redisutil.InvalidateWorkspaces(ctx, s.redis, slog.Default(), userIDs...)
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

func normalizeUpdateNursery(input UpdateNurseryRequest) UpdateNurseryRequest {
	input.Name = strings.TrimSpace(input.Name)
	if input.BrandIconKey != nil {
		s := strings.ToLower(strings.TrimSpace(*input.BrandIconKey))
		input.BrandIconKey = &s
	}
	if input.BrandColor != nil {
		s := strings.ToUpper(strings.TrimSpace(*input.BrandColor))
		input.BrandColor = &s
	}
	if input.LogoURL != nil {
		s := strings.TrimSpace(*input.LogoURL)
		input.LogoURL = &s
	}
	return input
}

func validateUpdateNursery(input UpdateNurseryRequest) error {
	if input.Status != nil {
		status := strings.ToUpper(strings.TrimSpace(*input.Status))
		switch status {
		case "ACTIVE", "INACTIVE", "SUSPENDED", "DELETED", "PENDING", "APPROVED", "REJECTED":
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// validBrandColors is the curated palette. Values are uppercased 7-char hex.
var validBrandColors = map[string]bool{
	"#2E7D32": true, // deep forest green (default)
	"#388E3C": true,
	"#43A047": true,
	"#66BB6A": true,
	"#F9A825": true, // amber
	"#EF6C00": true, // orange
	"#5D4037": true, // brown
	"#1565C0": true, // blue
	"#6A1B9A": true, // purple
	"#37474F": true, // dark slate
}

// validBrandIconKeys is the allowed set of preset icon identifiers.
var validBrandIconKeys = map[string]bool{
	"leaf": true, "tree": true, "flower": true, "seedling": true, "pot": true,
	"cactus": true, "palm": true, "bonsai": true, "herb": true, "lotus": true,
}

var (
	ErrInvalidBrandColor   = errors.New("brand_color is not in the allowed palette")
	ErrInvalidBrandIconKey = errors.New("brand_icon_key is not a valid preset icon")
	ErrInvalidLogoURL      = errors.New("logo_url must be a valid URL pointing to the nursery-logos bucket")
	ErrBrandingConflict    = errors.New("logo_url and brand_icon_key cannot both be set")
)

func validateBranding(input UpdateNurseryRequest) error {
	hasLogo := input.LogoURL != nil && *input.LogoURL != ""
	hasIcon := input.BrandIconKey != nil && *input.BrandIconKey != ""

	if hasLogo && hasIcon {
		return ErrBrandingConflict
	}
	if hasLogo {
		u := *input.LogoURL
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return ErrInvalidLogoURL
		}
		if !strings.Contains(u, "nursery-logos") {
			return ErrInvalidLogoURL
		}
	}
	if hasIcon && !validBrandIconKeys[*input.BrandIconKey] {
		return ErrInvalidBrandIconKey
	}
	if input.BrandColor != nil && *input.BrandColor != "" {
		if !validBrandColors[*input.BrandColor] {
			return ErrInvalidBrandColor
		}
	}
	// Mutual exclusion: setting logo clears icon, setting icon clears logo (done at DB level via $11/$12).
	return nil
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
	if input.GPSAccuracyM != nil && *input.GPSAccuracyM < 0 {
		return ErrInvalidAddress
	}
	if input.LocationSource != nil && !isAllowedLocationSource(*input.LocationSource) {
		return ErrInvalidAddress
	}
	return nil
}

func markConfirmedLocation(actor ActorContext, input AddressRequest) AddressRequest {
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

func canManageNurseries(actor ActorContext) bool {
	return actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") || actor.HasRole("NURSERY_OWNER")
}

type NurseryActor struct {
	UserID int64
	Roles  []string
}

func actorFromContext(actor ActorContext) NurseryActor {
	return NurseryActor{UserID: actor.UserID, Roles: actor.Roles}
}

func enrichNursery(actor NurseryActor, nursery Nursery) Nursery {
	status := strings.ToUpper(strings.TrimSpace(nursery.Status))
	isAdmin := hasNurseryRole(actor.Roles, "ADMIN") || hasNurseryRole(actor.Roles, "SUPER_ADMIN")
	isOwner := nursery.OwnerUserID != nil && actor.UserID > 0 && *nursery.OwnerUserID == actor.UserID
	active := status == "APPROVED" || status == "ACTIVE"
	deleted := status == "DELETED"

	nursery.Lifecycle = nurseryPtr(lifecycle.Nursery(status))
	nursery.Summary = &NurserySummary{
		IsOwner:     isOwner,
		IsApproved:  active,
		IsPending:   status == "PENDING",
		IsSuspended: status == "SUSPENDED",
		IsDeleted:   deleted,
	}
	if actor.UserID > 0 || len(actor.Roles) > 0 {
		nursery.Capabilities = &NurseryCapabilities{
			CanEdit:            (isAdmin || isOwner) && !deleted,
			CanDelete:          isAdmin && !deleted,
			CanApprove:         isAdmin && status == "PENDING",
			CanReject:          isAdmin && status == "PENDING",
			CanSuspend:         isAdmin && active,
			CanReactivate:      isAdmin && status == "SUSPENDED",
			CanManageInventory: (isAdmin || isOwner) && active,
			CanManageUsers:     (isAdmin || isOwner) && !deleted,
			CanManageAddresses: (isAdmin || isOwner) && !deleted,
		}
	}
	return nursery
}

func hasNurseryRole(roles []string, role string) bool {
	for _, item := range roles {
		if strings.EqualFold(item, role) {
			return true
		}
	}
	return false
}

func nurseryPtr[T any](value T) *T {
	return &value
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
