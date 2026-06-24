package dispatches

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	ErrForbidden    = errors.New("forbidden")
	ErrInvalidInput = errors.New("invalid input")
	ErrDuplicate    = errors.New("duplicate dispatch")
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(ctx context.Context, actor ActorContext, input ListDispatchesRequest) ([]Dispatch, Pagination, error) {
	input = normalizeList(input)
	if err := s.scopeList(ctx, actor, &input); err != nil {
		return nil, Pagination{}, err
	}
	dispatches, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	return dispatches, Pagination{Page: input.Page, PerPage: input.PerPage, Total: total, TotalPages: totalPages(total, input.PerPage)}, nil
}

func (s *Service) Get(ctx context.Context, actor ActorContext, dispatchID int64) (Dispatch, error) {
	dispatch, err := s.repository.FindByID(ctx, dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	if err := s.canAccess(ctx, actor, *dispatch); err != nil {
		return Dispatch{}, err
	}
	return *dispatch, nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, req CreateDispatchRequest) (Dispatch, error) {
	input, err := normalizeCreate(req)
	if err != nil {
		return Dispatch{}, err
	}
	access, err := s.repository.OrderAccess(ctx, input.OrderID)
	if err != nil {
		return Dispatch{}, err
	}
	if err := s.canAccessOrder(ctx, actor, access); err != nil {
		return Dispatch{}, err
	}
	if input.DispatchNumber != nil {
		duplicate, err := s.repository.HasDuplicate(ctx, *input.DispatchNumber)
		if err != nil {
			return Dispatch{}, err
		}
		if duplicate {
			return Dispatch{}, ErrDuplicate
		}
	}
	dispatch, err := s.repository.Create(ctx, actor.UserID, input)
	if err != nil {
		return Dispatch{}, err
	}
	s.audit(ctx, actor, "dispatches", dispatch.ID, actionInsert, req)
	return *dispatch, nil
}

func (s *Service) UpdateStatus(ctx context.Context, actor ActorContext, dispatchID int64, req UpdateStatusRequest) (Dispatch, error) {
	current, err := s.repository.FindByID(ctx, dispatchID)
	if err != nil {
		return Dispatch{}, err
	}
	if err := s.canAccess(ctx, actor, *current); err != nil {
		return Dispatch{}, err
	}
	status := strings.ToUpper(strings.TrimSpace(req.Status))
	if !isAllowedStatus(status) {
		return Dispatch{}, ErrInvalidInput
	}
	deliveryDate, err := parseOptionalTime(req.DeliveryDate)
	if err != nil {
		return Dispatch{}, ErrInvalidInput
	}
	dispatch, err := s.repository.UpdateStatus(ctx, dispatchID, UpdateStatusInput{Status: status, DeliveryDate: deliveryDate, Notes: req.Notes})
	if err != nil {
		return Dispatch{}, err
	}
	s.audit(ctx, actor, "dispatches", dispatch.ID, actionUpdate, req)
	return *dispatch, nil
}

func (s *Service) CreateItem(ctx context.Context, actor ActorContext, dispatchID int64, req DispatchItemRequest) (DispatchItem, error) {
	dispatch, err := s.repository.FindByID(ctx, dispatchID)
	if err != nil {
		return DispatchItem{}, err
	}
	if err := s.canAccess(ctx, actor, *dispatch); err != nil {
		return DispatchItem{}, err
	}
	if req.Quantity <= 0 {
		return DispatchItem{}, ErrInvalidInput
	}
	item, err := s.repository.CreateItem(ctx, dispatchID, req)
	if err != nil {
		return DispatchItem{}, err
	}
	s.audit(ctx, actor, "dispatch_items", item.ID, actionInsert, req)
	return *item, nil
}

func (s *Service) scopeList(ctx context.Context, actor ActorContext, input *ListDispatchesRequest) error {
	if hasRole(actor, "ADMIN") {
		return nil
	}
	if hasRole(actor, "NURSERY_OWNER") {
		if input.OrderID > 0 {
			access, err := s.repository.OrderAccess(ctx, input.OrderID)
			if err != nil {
				return err
			}
			return s.canAccessOrder(ctx, actor, access)
		}
		if input.NurseryID <= 0 {
			return ErrForbidden
		}
		member, err := s.repository.IsNurseryMember(ctx, input.NurseryID, actor.UserID)
		if err != nil {
			return err
		}
		if !member {
			return ErrForbidden
		}
		return nil
	}
	if hasRole(actor, "DRIVER") {
		input.DriverUserID = actor.UserID
		return nil
	}
	return ErrForbidden
}

func (s *Service) canAccess(ctx context.Context, actor ActorContext, dispatch Dispatch) error {
	if hasRole(actor, "ADMIN") {
		return nil
	}
	if hasRole(actor, "NURSERY_OWNER") && dispatch.SellerNurseryID != nil {
		member, err := s.repository.IsNurseryMember(ctx, *dispatch.SellerNurseryID, actor.UserID)
		if err != nil {
			return err
		}
		if member {
			return nil
		}
	}
	if hasRole(actor, "DRIVER") && dispatch.DriverID != nil {
		isDriver, err := s.repository.IsDispatchDriver(ctx, *dispatch.DriverID, actor.UserID)
		if err == nil && isDriver {
			return nil
		}
	}
	return ErrForbidden
}

func (s *Service) canAccessOrder(ctx context.Context, actor ActorContext, access *OrderAccess) error {
	if hasRole(actor, "ADMIN") {
		return nil
	}
	if hasRole(actor, "NURSERY_OWNER") && access.NurseryID != nil {
		member, err := s.repository.IsNurseryMember(ctx, *access.NurseryID, actor.UserID)
		if err != nil {
			return err
		}
		if member {
			return nil
		}
	}
	return ErrForbidden
}

func normalizeCreate(req CreateDispatchRequest) (CreateDispatchInput, error) {
	if req.OrderID <= 0 {
		return CreateDispatchInput{}, ErrInvalidInput
	}
	dispatchDate, err := parseOptionalTime(req.DispatchDate)
	if err != nil {
		return CreateDispatchInput{}, ErrInvalidInput
	}
	for _, item := range req.Items {
		if item.Quantity <= 0 {
			return CreateDispatchInput{}, ErrInvalidInput
		}
	}
	if req.DispatchNumber == nil || strings.TrimSpace(*req.DispatchNumber) == "" {
		number := fmt.Sprintf("GR-DSP-%d", time.Now().UnixNano())
		req.DispatchNumber = &number
	}
	return CreateDispatchInput{OrderID: req.OrderID, DispatchNumber: req.DispatchNumber, VehicleID: req.VehicleID, DriverID: req.DriverID, DispatchDate: dispatchDate, DestinationAddress: req.DestinationAddress, Notes: req.Notes, Items: req.Items}, nil
}

func normalizeList(input ListDispatchesRequest) ListDispatchesRequest {
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

func parseOptionalTime(value *string) (*time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	text := strings.TrimSpace(*value)
	if parsed, err := time.Parse(time.RFC3339, text); err == nil {
		return &parsed, nil
	}
	parsed, err := time.Parse(time.DateOnly, text)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func isAllowedStatus(status string) bool {
	switch status {
	case "PENDING", "DISPATCHED", "IN_TRANSIT", "DELIVERED", "CANCELLED":
		return true
	default:
		return false
	}
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

func (s *Service) audit(ctx context.Context, actor ActorContext, table string, recordID int64, action string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = s.repository.CreateAuditLog(ctx, CreateAuditInput{TableName: table, RecordID: recordID, Action: action, ChangedBy: actor.UserID, SourceIP: actor.IPAddress, UserAgent: actor.UserAgent, NewJSON: string(data), At: time.Now()})
}
