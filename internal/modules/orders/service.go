package orders

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
	ErrForbidden     = errors.New("forbidden")
	ErrInvalidInput  = errors.New("invalid input")
	ErrInvalidStatus = errors.New("invalid status transition")
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(ctx context.Context, actor ActorContext, input ListOrdersRequest) ([]Order, Pagination, error) {
	input = normalizeList(input)
	if err := s.scopeList(ctx, actor, &input); err != nil {
		return nil, Pagination{}, err
	}
	orders, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	return orders, Pagination{Page: input.Page, PerPage: input.PerPage, Total: total, TotalPages: totalPages(total, input.PerPage)}, nil
}

func (s *Service) Get(ctx context.Context, actor ActorContext, orderID int64) (Order, error) {
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if err := s.canView(ctx, actor, *order); err != nil {
		return Order{}, err
	}
	return *order, nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, input CreateOrderRequest) (Order, error) {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return Order{}, ErrForbidden
	}
	if input.BuyerMobile != nil && *input.BuyerMobile != "" {
		buyerID, err := s.repository.FindOrCreateBuyerByMobile(ctx, *input.BuyerMobile, stringOrEmpty(input.BuyerName))
		if err != nil {
			return Order{}, err
		}
		input.BuyerUserID = &buyerID
	}
	input = normalizeCreate(actor, input)
	if err := validateOrder(input); err != nil {
		return Order{}, err
	}
	if err := s.canCreate(ctx, actor, input); err != nil {
		return Order{}, err
	}
	orderNumber := strings.TrimSpace(stringOrEmpty(input.OrderNumber))
	if orderNumber == "" {
		orderNumber = fmt.Sprintf("GR-ORD-%d", time.Now().UnixNano())
	}
	order, err := s.repository.Create(ctx, actor.UserID, input, orderNumber)
	if err != nil {
		return Order{}, err
	}
	s.audit(ctx, actor, "orders", order.ID, actionInsert, input)
	return *order, nil
}

func (s *Service) UpdateStatus(ctx context.Context, actor ActorContext, orderID int64, input UpdateStatusRequest) (Order, error) {
	status := strings.ToUpper(strings.TrimSpace(input.Status))
	if !isAllowedStatus(status) {
		return Order{}, ErrInvalidInput
	}
	current, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if err := s.canManage(ctx, actor, *current); err != nil {
		return Order{}, err
	}
	order, err := s.repository.UpdateStatus(ctx, actor.UserID, orderID, status)
	if err != nil {
		return Order{}, err
	}
	s.audit(ctx, actor, "orders", order.ID, actionUpdate, map[string]any{"order_status": status})
	return *order, nil
}

// StartLoading marks an order as LOADING (nursery owner or assigned manager only).
func (s *Service) StartLoading(ctx context.Context, actor ActorContext, orderID int64) (Order, error) {
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if order.Status != "CONFIRMED" && order.Status != "DRAFT" {
		return Order{}, ErrInvalidStatus
	}
	if err := s.canManage(ctx, actor, *order); err != nil {
		return Order{}, err
	}
	updated, err := s.repository.UpdateStatusWithLoading(ctx, actor.UserID, orderID, "LOADING", "start")
	if err != nil {
		return Order{}, err
	}
	s.audit(ctx, actor, "orders", orderID, actionUpdate, map[string]any{"order_status": "LOADING"})
	return *updated, nil
}

// CompleteLoading marks an order as LOADED.
func (s *Service) CompleteLoading(ctx context.Context, actor ActorContext, orderID int64) (Order, error) {
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if order.Status != "LOADING" {
		return Order{}, ErrInvalidStatus
	}
	if err := s.canManage(ctx, actor, *order); err != nil {
		return Order{}, err
	}
	updated, err := s.repository.UpdateStatusWithLoading(ctx, actor.UserID, orderID, "LOADED", "complete")
	if err != nil {
		return Order{}, err
	}
	s.audit(ctx, actor, "orders", orderID, actionUpdate, map[string]any{"order_status": "LOADED"})
	return *updated, nil
}

// Cancel cancels an order.
func (s *Service) Cancel(ctx context.Context, actor ActorContext, orderID int64, reason string) (Order, error) {
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if order.Status == "CANCELLED" || order.Status == "DELIVERED" {
		return Order{}, ErrInvalidStatus
	}
	if err := s.canManage(ctx, actor, *order); err != nil {
		return Order{}, err
	}
	updated, err := s.repository.Cancel(ctx, actor.UserID, orderID, reason)
	if err != nil {
		return Order{}, err
	}
	s.audit(ctx, actor, "orders", orderID, actionUpdate, map[string]any{"order_status": "CANCELLED", "reason": reason})
	return *updated, nil
}

// AssignManager assigns a manager to an order (owner or admin only).
func (s *Service) AssignManager(ctx context.Context, actor ActorContext, orderID int64, managerUserID int64) (Order, error) {
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if !hasRole(actor, "ADMIN") && !hasRole(actor, "SUPER_ADMIN") {
		nurseryID := order.NurseryID
		if nurseryID == nil {
			nurseryID = order.SellerNurseryID
		}
		if nurseryID == nil {
			return Order{}, ErrForbidden
		}
		owner, err := s.repository.IsNurseryOwner(ctx, *nurseryID, actor.UserID)
		if err != nil || !owner {
			return Order{}, ErrForbidden
		}
	}
	updated, err := s.repository.AssignManager(ctx, orderID, managerUserID)
	if err != nil {
		return Order{}, err
	}
	s.audit(ctx, actor, "orders", orderID, actionUpdate, map[string]any{"assigned_manager_user_id": managerUserID})
	return *updated, nil
}

func (s *Service) Delete(ctx context.Context, actor ActorContext, orderID int64) error {
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return err
	}
	if err := s.canManage(ctx, actor, *order); err != nil {
		return err
	}
	if err := s.repository.Delete(ctx, orderID); err != nil {
		return err
	}
	s.audit(ctx, actor, "orders", orderID, actionDelete, map[string]any{"deleted": true})
	return nil
}

func (s *Service) ListItems(ctx context.Context, actor ActorContext, orderID int64) ([]OrderItem, error) {
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if err := s.canView(ctx, actor, *order); err != nil {
		return nil, err
	}
	return s.repository.ListItems(ctx, orderID)
}

func (s *Service) CreateItem(ctx context.Context, actor ActorContext, orderID int64, input OrderItemRequest) (OrderItem, error) {
	order, err := s.repository.FindByID(ctx, orderID)
	if err != nil {
		return OrderItem{}, err
	}
	if err := s.canManage(ctx, actor, *order); err != nil {
		return OrderItem{}, err
	}
	if err := validateItem(input); err != nil {
		return OrderItem{}, err
	}
	item, err := s.repository.CreateItem(ctx, orderID, input)
	if err != nil {
		return OrderItem{}, err
	}
	s.audit(ctx, actor, "order_items", item.ID, actionInsert, input)
	return *item, nil
}

func (s *Service) UpdateItem(ctx context.Context, actor ActorContext, itemID int64, input OrderItemRequest) (OrderItem, error) {
	if err := validateItem(input); err != nil {
		return OrderItem{}, err
	}
	current, err := s.repository.FindItem(ctx, itemID)
	if err != nil {
		return OrderItem{}, err
	}
	order, err := s.repository.FindByID(ctx, current.OrderID)
	if err != nil {
		return OrderItem{}, err
	}
	if err := s.canManage(ctx, actor, *order); err != nil {
		return OrderItem{}, err
	}
	item, err := s.repository.UpdateItem(ctx, itemID, input)
	if err != nil {
		return OrderItem{}, err
	}
	s.audit(ctx, actor, "order_items", item.ID, actionUpdate, input)
	return *item, nil
}

func (s *Service) DeleteItem(ctx context.Context, actor ActorContext, itemID int64) error {
	item, err := s.repository.FindItem(ctx, itemID)
	if err != nil {
		return err
	}
	order, err := s.repository.FindByID(ctx, item.OrderID)
	if err != nil {
		return err
	}
	if err := s.canManage(ctx, actor, *order); err != nil {
		return err
	}
	if err := s.repository.DeleteItem(ctx, itemID); err != nil {
		return err
	}
	s.audit(ctx, actor, "order_items", itemID, actionDelete, map[string]any{"deleted": true})
	return nil
}

func (s *Service) scopeList(ctx context.Context, actor ActorContext, input *ListOrdersRequest) error {
	if hasRole(actor, "ADMIN") {
		return nil
	}
	if input.Buying {
		// Buyer perspective: filter by buyer_user_id OR buyer_nursery_id
		input.BuyerID = actor.UserID
		if hasRole(actor, "NURSERY_OWNER") {
			nurseryID, _ := s.repository.GetOwnedNurseryID(ctx, actor.UserID)
			if nurseryID != nil {
				input.NurseryID = *nurseryID
			}
		}
		return nil
	}
	if hasRole(actor, "NURSERY_OWNER") || hasRole(actor, "MANAGER") {
		if input.NurseryID > 0 {
			return s.mustBeNurseryMember(ctx, actor, input.NurseryID)
		}
		nurseryIDs, err := s.repository.GetUserNurseryIDs(ctx, actor.UserID)
		if err != nil {
			return err
		}
		if len(nurseryIDs) == 0 {
			return ErrForbidden
		}
		input.NurseryID = nurseryIDs[0]
		return nil
	}
	input.BuyerID = actor.UserID
	return nil
}

func (s *Service) canView(ctx context.Context, actor ActorContext, order Order) error {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return nil
	}
	if order.CustomerUserID != nil && *order.CustomerUserID == actor.UserID {
		return nil
	}
	if order.BuyerUserID != nil && *order.BuyerUserID == actor.UserID {
		return nil
	}
	if order.BuyerNurseryID != nil {
		isOwner, _ := s.repository.IsNurseryOwner(ctx, *order.BuyerNurseryID, actor.UserID)
		if isOwner {
			return nil
		}
	}
	if order.AssignedManagerUserID != nil && *order.AssignedManagerUserID == actor.UserID {
		return nil
	}
	nurseryID := order.NurseryID
	if nurseryID == nil {
		nurseryID = order.SellerNurseryID
	}
	if nurseryID != nil {
		owner, _ := s.repository.IsNurseryOwner(ctx, *nurseryID, actor.UserID)
		if owner {
			return nil
		}
		if err := s.mustBeNurseryMember(ctx, actor, *nurseryID); err == nil {
			return nil
		}
	}
	return ErrForbidden
}

func (s *Service) canCreate(ctx context.Context, actor ActorContext, input CreateOrderRequest) error {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return ErrForbidden
	}
	if hasRole(actor, "NURSERY_OWNER") || hasRole(actor, "MANAGER") {
		if input.SellerNurseryID == nil {
			return ErrInvalidInput
		}
		return s.mustBeNurseryMember(ctx, actor, *input.SellerNurseryID)
	}
	// BUYER creating own order
	if input.BuyerUserID != nil && *input.BuyerUserID != actor.UserID {
		return ErrForbidden
	}
	return nil
}

func (s *Service) canManage(ctx context.Context, actor ActorContext, order Order) error {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return nil
	}
	if order.AssignedManagerUserID != nil && *order.AssignedManagerUserID == actor.UserID {
		return nil
	}
	nurseryID := order.NurseryID
	if nurseryID == nil {
		nurseryID = order.SellerNurseryID
	}
	if nurseryID != nil {
		owner, _ := s.repository.IsNurseryOwner(ctx, *nurseryID, actor.UserID)
		if owner {
			return nil
		}
		if err := s.mustBeNurseryMember(ctx, actor, *nurseryID); err == nil {
			return nil
		}
	}
	return ErrForbidden
}

func (s *Service) mustBeNurseryMember(ctx context.Context, actor ActorContext, nurseryID int64) error {
	member, err := s.repository.IsNurseryMember(ctx, nurseryID, actor.UserID)
	if err != nil {
		return err
	}
	if !member {
		return ErrForbidden
	}
	return nil
}

func normalizeList(input ListOrdersRequest) ListOrdersRequest {
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
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	input.SortBy = strings.TrimSpace(input.SortBy)
	input.SortOrder = strings.ToLower(strings.TrimSpace(input.SortOrder))
	if input.SortOrder != "asc" && input.SortOrder != "desc" {
		input.SortOrder = "desc"
	}
	return input
}

func normalizeCreate(actor ActorContext, input CreateOrderRequest) CreateOrderRequest {
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "PENDING"
	}
	// Only default buyer to self when a BUYER is creating their own order
	if input.BuyerUserID == nil && hasRole(actor, "BUYER") {
		input.BuyerUserID = &actor.UserID
	}
	return input
}

func validateOrder(input CreateOrderRequest) error {
	if input.BuyerUserID == nil || *input.BuyerUserID <= 0 || input.SellerNurseryID == nil || *input.SellerNurseryID <= 0 {
		return ErrInvalidInput
	}
	if !isAllowedStatus(input.Status) {
		return ErrInvalidInput
	}
	for _, item := range input.Items {
		if err := validateItem(item); err != nil {
			return err
		}
	}
	return nil
}

func validateItem(input OrderItemRequest) error {
	if input.PlantID <= 0 || input.Quantity <= 0 || input.UnitPrice < 0 || input.TotalPrice < 0 {
		return ErrInvalidInput
	}
	expected := input.Quantity * input.UnitPrice
	if math.Abs(expected-input.TotalPrice) > 0.01 {
		return ErrInvalidInput
	}
	return nil
}

func isAllowedStatus(value string) bool {
	switch value {
	case "PENDING", "CONFIRMED", "PARTIALLY_FULFILLED", "COMPLETED", "CANCELLED":
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
