package requests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrForbidden             = errors.New("forbidden")
	ErrInvalidInput          = errors.New("invalid input")
	ErrInsufficientInventory = errors.New("insufficient inventory")
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
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
	s.audit(ctx, actor, "plant_requests", requestID, actionDelete, map[string]any{"status": "CANCELLED"})
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

func (s *Service) CreateResponse(ctx context.Context, actor ActorContext, requestID int64, input CreateResponseRequest) (Response, error) {
	input = normalizeResponse(input)
	if err := s.canManageNursery(ctx, actor, input.SupplierNurseryID); err != nil {
		return Response{}, err
	}
	request, err := s.repository.FindByID(ctx, requestID)
	if err != nil {
		return Response{}, err
	}
	if err := validateResponse(input); err != nil {
		return Response{}, err
	}
	available, err := s.repository.InventoryAvailable(ctx, input.SupplierNurseryID, request.PlantID, request.SizeID)
	if err != nil {
		return Response{}, err
	}
	if available < input.AvailableQuantity {
		return Response{}, ErrInsufficientInventory
	}
	response, err := s.repository.CreateResponse(ctx, requestID, actor.UserID, input)
	if err != nil {
		return Response{}, err
	}
	s.audit(ctx, actor, "plant_request_responses", response.ID, actionInsert, input)
	return *response, nil
}

func (s *Service) UpdateResponse(ctx context.Context, actor ActorContext, responseID int64, input UpdateResponseRequest) (Response, error) {
	if !canUseRequests(actor) {
		return Response{}, ErrForbidden
	}
	input = normalizeUpdateResponse(input)
	if err := validateUpdateResponse(input); err != nil {
		return Response{}, err
	}
	response, err := s.repository.UpdateResponse(ctx, responseID, input)
	if err != nil {
		return Response{}, err
	}
	s.audit(ctx, actor, "plant_request_responses", response.ID, actionUpdate, input)
	return *response, nil
}

func (s *Service) canManageNursery(ctx context.Context, actor ActorContext, nurseryID int64) error {
	if hasRole(actor, "ADMIN") {
		return nil
	}
	if hasRole(actor, "NURSERY_OWNER") {
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
	return hasRole(actor, "ADMIN") || hasRole(actor, "NURSERY_OWNER")
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

func normalizeResponse(input CreateResponseRequest) CreateResponseRequest {
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "RESPONDED"
	}
	return input
}

func normalizeUpdateResponse(input UpdateResponseRequest) UpdateResponseRequest {
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "RESPONDED"
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

func validateResponse(input CreateResponseRequest) error {
	if input.SupplierNurseryID <= 0 || input.AvailableQuantity <= 0 {
		return ErrInvalidInput
	}
	if !isAllowedResponseStatus(input.Status) {
		return ErrInvalidInput
	}
	return nil
}

func validateUpdateResponse(input UpdateResponseRequest) error {
	if input.AvailableQuantity < 0 {
		return ErrInvalidInput
	}
	if !isAllowedResponseStatus(input.Status) {
		return ErrInvalidInput
	}
	return nil
}

func isAllowedRequestStatus(value string) bool {
	switch value {
	case "OPEN", "RESPONDED", "FULFILLED", "CANCELLED", "EXPIRED":
		return true
	default:
		return false
	}
}

func isAllowedResponseStatus(value string) bool {
	switch value {
	case "RESPONDED", "ACCEPTED", "REJECTED", "EXPIRED":
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

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(data)
}
