package drivers

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/redis/go-redis/v9"
)

var (
	ErrForbidden           = errors.New("forbidden")
	ErrInvalidInput        = errors.New("invalid input")
	ErrDuplicate           = errors.New("duplicate driver")
	ErrOwnerCannotBeDriver = errors.New("nursery owners cannot register as a driver")
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

func (s *Service) List(ctx context.Context, actor ActorContext, input ListDriversRequest) ([]Driver, Pagination, error) {
	if !hasRole(actor, "ADMIN") {
		return nil, Pagination{}, ErrForbidden
	}
	input = normalizeList(input)
	drivers, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	for i := range drivers {
		drivers[i] = enrichDriver(actor, drivers[i])
	}
	return drivers, Pagination{Page: input.Page, PerPage: input.PerPage, Total: total, TotalPages: totalPages(total, input.PerPage)}, nil
}

func (s *Service) Get(ctx context.Context, actor ActorContext, driverID int64) (Driver, error) {
	driver, err := s.repository.FindByID(ctx, driverID)
	if err != nil {
		return Driver{}, err
	}
	if !hasRole(actor, "ADMIN") && (driver.UserID == nil || *driver.UserID != actor.UserID) {
		return Driver{}, ErrForbidden
	}
	return enrichDriver(actor, *driver), nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, req DriverRequest) (Driver, error) {
	if !hasRole(actor, "ADMIN") {
		return Driver{}, ErrForbidden
	}
	input, err := normalizeDriver(req)
	if err != nil {
		return Driver{}, err
	}
	duplicate, err := s.repository.HasDuplicate(ctx, input, 0)
	if err != nil {
		return Driver{}, err
	}
	if duplicate {
		return Driver{}, ErrDuplicate
	}
	driver, err := s.repository.Create(ctx, input)
	if err != nil {
		return Driver{}, err
	}
	s.audit(ctx, actor, driver.ID, actionInsert, req)
	return enrichDriver(actor, *driver), nil
}

func (s *Service) Update(ctx context.Context, actor ActorContext, driverID int64, req DriverRequest) (Driver, error) {
	if !hasRole(actor, "ADMIN") {
		return Driver{}, ErrForbidden
	}
	input, err := normalizeDriver(req)
	if err != nil {
		return Driver{}, err
	}
	duplicate, err := s.repository.HasDuplicate(ctx, input, driverID)
	if err != nil {
		return Driver{}, err
	}
	if duplicate {
		return Driver{}, ErrDuplicate
	}
	driver, err := s.repository.Update(ctx, driverID, input)
	if err != nil {
		return Driver{}, err
	}
	s.audit(ctx, actor, driver.ID, actionUpdate, req)
	return enrichDriver(actor, *driver), nil
}

func (s *Service) Delete(ctx context.Context, actor ActorContext, driverID int64) error {
	if !hasRole(actor, "ADMIN") {
		return ErrForbidden
	}
	if err := s.repository.Delete(ctx, driverID); err != nil {
		return err
	}
	s.audit(ctx, actor, driverID, actionDelete, map[string]any{"status": "INACTIVE"})
	return nil
}

// Apply creates or updates the current user's driver profile (V1 self-registration flow).
func (s *Service) Apply(ctx context.Context, actor ActorContext, req ApplyDriverRequest) (Driver, error) {
	if strings.TrimSpace(req.LicenceNumber) == "" || strings.TrimSpace(req.VehicleNumber) == "" {
		return Driver{}, ErrInvalidInput
	}
	ownsNursery, err := s.repository.UserOwnsANursery(ctx, actor.UserID)
	if err != nil {
		return Driver{}, err
	}
	if ownsNursery {
		return Driver{}, ErrOwnerCannotBeDriver
	}
	driver, err := s.repository.Upsert(ctx, actor.UserID, req)
	if err != nil {
		return Driver{}, err
	}
	s.audit(ctx, actor, driver.ID, actionInsert, req)
	return enrichDriver(actor, *driver), nil
}

// GetMine returns the driver profile for the current user.
func (s *Service) GetMine(ctx context.Context, actor ActorContext) (Driver, error) {
	driver, err := s.repository.FindByUserID(ctx, actor.UserID)
	if err != nil {
		return Driver{}, err
	}
	return enrichDriver(actor, *driver), nil
}

// Approve approves a driver profile (admin only).
func (s *Service) Approve(ctx context.Context, actor ActorContext, driverUserID int64) (Driver, error) {
	if !hasRole(actor, "ADMIN") && !hasRole(actor, "SUPER_ADMIN") {
		return Driver{}, ErrForbidden
	}
	driver, err := s.repository.Approve(ctx, driverUserID, actor.UserID)
	if err != nil {
		return Driver{}, err
	}
	redisutil.InvalidateWorkspaces(ctx, s.redis, slog.Default(), driverUserID)
	s.audit(ctx, actor, driver.ID, actionUpdate, map[string]any{"approval_status": "APPROVED"})
	return enrichDriver(actor, *driver), nil
}

func (s *Service) CreateLocation(ctx context.Context, actor ActorContext, driverID int64, input LocationRequest) (DriverLocation, error) {
	driver, err := s.repository.FindByID(ctx, driverID)
	if err != nil {
		return DriverLocation{}, err
	}
	if !hasRole(actor, "ADMIN") && (driver.UserID == nil || *driver.UserID != actor.UserID) {
		return DriverLocation{}, ErrForbidden
	}
	if input.Latitude < -90 || input.Latitude > 90 || input.Longitude < -180 || input.Longitude > 180 {
		return DriverLocation{}, ErrInvalidInput
	}
	location, err := s.repository.CreateLocation(ctx, driverID, actor.UserID, input)
	if err != nil {
		return DriverLocation{}, err
	}
	s.audit(ctx, actor, driverID, actionUpdate, input)
	return *location, nil
}

func normalizeDriver(req DriverRequest) (DriverInput, error) {
	status := statusOrActive(req.Status)
	if !isAllowedStatus(status) {
		return DriverInput{}, ErrInvalidInput
	}
	var expiry *time.Time
	if req.LicenseExpiryDate != nil && strings.TrimSpace(*req.LicenseExpiryDate) != "" {
		parsed, err := time.Parse(time.DateOnly, strings.TrimSpace(*req.LicenseExpiryDate))
		if err != nil {
			return DriverInput{}, ErrInvalidInput
		}
		expiry = &parsed
	}
	return DriverInput{UserID: req.UserID, LicenseNumber: req.LicenseNumber, LicenseExpiryDate: expiry, EmergencyContact: req.EmergencyContact, Status: status}, nil
}

func normalizeList(input ListDriversRequest) ListDriversRequest {
	if input.Page <= 0 {
		input.Page = 1
	}
	if input.PerPage <= 0 {
		input.PerPage = 20
	}
	if input.PerPage > 100 {
		input.PerPage = 100
	}
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	input.Search = strings.TrimSpace(input.Search)
	input.SortBy = strings.TrimSpace(input.SortBy)
	input.SortOrder = strings.ToLower(strings.TrimSpace(input.SortOrder))
	if input.SortOrder != "asc" && input.SortOrder != "desc" {
		input.SortOrder = "desc"
	}
	return input
}

func isAllowedStatus(status string) bool {
	switch status {
	case "ACTIVE", "INACTIVE", "SUSPENDED", "DELETED":
		return true
	default:
		return false
	}
}

func enrichDriver(actor ActorContext, driver Driver) Driver {
	status := strings.ToUpper(strings.TrimSpace(driver.Status))
	approvalStatus := strings.ToUpper(strings.TrimSpace(driver.ApprovalStatus))
	profileStatus := strings.ToUpper(strings.TrimSpace(driver.ProfileStatus))
	isAdmin := hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN")
	isSelf := driver.UserID != nil && *driver.UserID == actor.UserID

	driver.Summary = &DriverSummary{
		IsApproved:        approvalStatus == "APPROVED",
		IsProfileComplete: profileStatus == "COMPLETE",
		IsActive:          status == "ACTIVE",
		IsSuspended:       status == "SUSPENDED",
	}
	driver.Capabilities = &DriverCapabilities{
		CanEdit:           isAdmin,
		CanDelete:         isAdmin,
		CanApprove:        isAdmin && approvalStatus != "APPROVED" && status != "DELETED",
		CanUpdateLocation: (isSelf || isAdmin) && approvalStatus == "APPROVED" && status == "ACTIVE",
	}
	return driver
}

func hasRole(actor ActorContext, role string) bool {
	for _, current := range actor.Roles {
		if strings.EqualFold(current, role) {
			return true
		}
	}
	return false
}

func totalPages(total int64, perPage int) int {
	if perPage <= 0 {
		return 0
	}
	return int(math.Ceil(float64(total) / float64(perPage)))
}

func (s *Service) audit(ctx context.Context, actor ActorContext, entityID int64, action auditlog.Action, newValue any) {
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:     actor.UserID,
		Module:     auditlog.ModuleDrivers,
		EntityType: "driver",
		EntityID:   entityID,
		Action:     action,
		NewValue:   newValue,
		IPAddress:  actor.IPAddress,
		DeviceInfo: actor.UserAgent,
	})
}
