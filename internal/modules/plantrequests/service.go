package plantrequests

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
)

var (
	ErrForbidden             = errors.New("forbidden")
	ErrInvalidInput          = errors.New("invalid input")
	ErrInsufficientInventory = errors.New("insufficient inventory")
	ErrInvalidTransition     = errors.New("invalid status transition")
)

type Service struct {
	repository Repository
	auditSvc   *auditlog.Service
}

func NewService(repository Repository, auditSvc *auditlog.Service) *Service {
	return &Service{repository: repository, auditSvc: auditSvc}
}

func (s *Service) List(ctx context.Context, actor ActorContext, input ListRequestsRequest) ([]PlantRequest, Pagination, error) {
	if !canUseRequests(actor) {
		return nil, Pagination{}, ErrForbidden
	}
	input = normalizeList(input)
	requests, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	return requests, Pagination{Page: input.Page, PerPage: input.PerPage, Total: total, TotalPages: totalPages(total, input.PerPage)}, nil
}

func (s *Service) Get(ctx context.Context, actor ActorContext, requestID int64) (PlantRequest, error) {
	if !canUseRequests(actor) {
		return PlantRequest{}, ErrForbidden
	}
	request, err := s.repository.FindByID(ctx, requestID)
	if err != nil {
		return PlantRequest{}, err
	}
	return *request, nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, input CreateRequest) (PlantRequest, error) {
	input = normalizeRequest(input)
	if err := s.canManageNursery(ctx, actor, input.RequestingNurseryID); err != nil {
		return PlantRequest{}, err
	}
	if err := validateRequest(input); err != nil {
		return PlantRequest{}, err
	}
	request, err := s.repository.Create(ctx, actor.UserID, input)
	if err != nil {
		return PlantRequest{}, err
	}
	s.audit(ctx, actor, "plant_requests", request.ID, actionInsert, input)
	return *request, nil
}

func (s *Service) Update(ctx context.Context, actor ActorContext, requestID int64, input UpdateRequest) (PlantRequest, error) {
	input = normalizeRequest(input)
	if err := s.canManageNursery(ctx, actor, input.RequestingNurseryID); err != nil {
		return PlantRequest{}, err
	}
	if err := validateRequest(input); err != nil {
		return PlantRequest{}, err
	}
	request, err := s.repository.Update(ctx, actor.UserID, requestID, input)
	if err != nil {
		return PlantRequest{}, err
	}
	s.audit(ctx, actor, "plant_requests", request.ID, actionUpdate, input)
	return *request, nil
}

// UpdateStatus advances the request lifecycle (OPEN → ACCEPTED, CLOSED, REJECTED, etc.)
// Only the requesting nursery manager or an admin may call this.
func (s *Service) UpdateStatus(ctx context.Context, actor ActorContext, requestID int64, input UpdateStatusRequest) (PlantRequest, error) {
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	if !isAllowedRequestStatus(input.Status) {
		return PlantRequest{}, ErrInvalidInput
	}
	existing, err := s.repository.FindByID(ctx, requestID)
	if err != nil {
		return PlantRequest{}, err
	}
	if err := s.canManageNursery(ctx, actor, existing.RequestingNurseryID); err != nil {
		return PlantRequest{}, err
	}
	request, err := s.repository.UpdateStatus(ctx, requestID, input.Status)
	if err != nil {
		return PlantRequest{}, err
	}
	s.audit(ctx, actor, "plant_requests", requestID, actionUpdate, input)
	return *request, nil
}

func (s *Service) Delete(ctx context.Context, actor ActorContext, requestID int64) error {
	request, err := s.repository.FindByID(ctx, requestID)
	if err != nil {
		return err
	}
	if err := s.canManageNursery(ctx, actor, request.RequestingNurseryID); err != nil {
		return err
	}
	if err := s.repository.Delete(ctx, requestID); err != nil {
		return err
	}
	s.audit(ctx, actor, "plant_requests", requestID, actionDelete, map[string]any{"status": "REJECTED"})
	return nil
}

func (s *Service) ListResponses(ctx context.Context, actor ActorContext, requestID int64) ([]Response, error) {
	if !canUseRequests(actor) {
		return nil, ErrForbidden
	}
	if _, err := s.repository.FindByID(ctx, requestID); err != nil {
		return nil, err
	}
	return s.repository.ListResponses(ctx, requestID)
}

// CreateResponse is called by the supplier nursery to declare their availability.
// Status must be AVAILABLE, PARTIAL, or NOT_AVAILABLE.
func (s *Service) CreateResponse(ctx context.Context, actor ActorContext, requestID int64, input CreateResponseRequest) (Response, error) {
	input = normalizeNewResponse(input)
	if err := s.canManageNursery(ctx, actor, input.SupplierNurseryID); err != nil {
		return Response{}, err
	}
	request, err := s.repository.FindByID(ctx, requestID)
	if err != nil {
		return Response{}, err
	}
	if request.RequestingNurseryID == input.SupplierNurseryID {
		return Response{}, ErrInvalidInput
	}
	if err := validateNewResponse(input); err != nil {
		return Response{}, err
	}
	if input.Status == "AVAILABLE" || input.Status == "PARTIAL" {
		available, err := s.repository.InventoryAvailable(ctx, input.SupplierNurseryID, request.PlantID, request.SizeID)
		if err != nil {
			return Response{}, err
		}
		if available < input.AvailableQuantity {
			return Response{}, ErrInsufficientInventory
		}
	}
	response, err := s.repository.CreateResponse(ctx, requestID, actor.UserID, input)
	if err != nil {
		return Response{}, err
	}
	s.audit(ctx, actor, "plant_request_responses", response.ID, actionInsert, input)
	return *response, nil
}

// UpdateResponse is called by the requesting nursery manager to select or reject a supplier.
// Status must be ACCEPTED or REJECTED.
func (s *Service) UpdateResponse(ctx context.Context, actor ActorContext, responseID int64, input UpdateResponseRequest) (Response, error) {
	if !canUseRequests(actor) {
		return Response{}, ErrForbidden
	}
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	if !isAllowedManagerResponseStatus(input.Status) {
		return Response{}, ErrInvalidInput
	}
	response, err := s.repository.UpdateResponse(ctx, responseID, input)
	if err != nil {
		return Response{}, err
	}
	// Recompute the parent request's status based on total accepted quantity.
	_ = s.repository.RecomputeRequestStatus(ctx, response.RequestID)
	s.audit(ctx, actor, "plant_request_responses", response.ID, actionUpdate, input)
	return *response, nil
}

func (s *Service) canManageNursery(ctx context.Context, actor ActorContext, nurseryID int64) error {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return nil
	}
	// Per business rules: both NURSERY_OWNER and MANAGER perform sourcing work
	if hasRole(actor, "NURSERY_OWNER") || hasRole(actor, "MANAGER") {
		member, err := s.repository.IsNurseryMember(ctx, nurseryID, actor.UserID)
		if err != nil {
			return err
		}
		if member {
			return nil
		}
	}
	return ErrForbidden
}

func canUseRequests(actor ActorContext) bool {
	// Per business rules: managers usually perform sourcing work and need full access
	return hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") ||
		hasRole(actor, "NURSERY_OWNER") || hasRole(actor, "MANAGER")
}

func normalizeList(input ListRequestsRequest) ListRequestsRequest {
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
	return input
}

func normalizeRequest(input CreateRequest) CreateRequest {
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "OPEN"
	}
	if input.RadiusKM == 0 {
		input.RadiusKM = 50
	}
	return input
}

func normalizeNewResponse(input CreateResponseRequest) CreateResponseRequest {
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "AVAILABLE"
	}
	return input
}

func validateRequest(input CreateRequest) error {
	if input.RequestingNurseryID <= 0 || input.PlantID <= 0 || input.QuantityRequired <= 0 || input.RadiusKM <= 0 {
		return ErrInvalidInput
	}
	if input.ExpiresAt != nil && input.ExpiresAt.Before(time.Now().Add(-time.Minute)) {
		return ErrInvalidInput
	}
	if !isAllowedRequestStatus(input.Status) {
		return ErrInvalidInput
	}
	return nil
}

func validateNewResponse(input CreateResponseRequest) error {
	if input.SupplierNurseryID <= 0 || input.AvailableQuantity <= 0 {
		return ErrInvalidInput
	}
	if !isAllowedNewResponseStatus(input.Status) {
		return ErrInvalidInput
	}
	return nil
}

// Request statuses
func isAllowedRequestStatus(value string) bool {
	switch value {
	case "DRAFT", "OPEN", "PARTIALLY_ACCEPTED", "ACCEPTED", "REJECTED", "CLOSED":
		return true
	default:
		return false
	}
}

// Supplier submits one of these when creating a response
func isAllowedNewResponseStatus(value string) bool {
	switch value {
	case "AVAILABLE", "PARTIAL", "NOT_AVAILABLE":
		return true
	default:
		return false
	}
}

// Manager selects one of these when updating a response
func isAllowedManagerResponseStatus(value string) bool {
	switch value {
	case "ACCEPTED", "REJECTED":
		return true
	default:
		return false
	}
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

func (s *Service) audit(ctx context.Context, actor ActorContext, entityType string, entityID int64, action auditlog.Action, newValue any) {
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:     actor.UserID,
		Module:     auditlog.ModuleRequests,
		EntityType: entityType,
		EntityID:   entityID,
		Action:     action,
		NewValue:   newValue,
		IPAddress:  actor.IPAddress,
		DeviceInfo: actor.UserAgent,
	})
}
