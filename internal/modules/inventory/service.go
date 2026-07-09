package inventory

import (
	"context"
	"errors"
	"strings"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
)

var (
	ErrForbidden    = errors.New("forbidden")
	ErrInvalidInput = errors.New("invalid input")
)

type Service struct {
	repository Repository
	auditSvc   *auditlog.Service
}

func NewService(repository Repository, auditSvc *auditlog.Service) *Service {
	return &Service{repository: repository, auditSvc: auditSvc}
}

func (s *Service) List(ctx context.Context, input ListInventoryRequest) ([]InventoryItem, Pagination, error) {
	input = normalizeList(input)
	items, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	return items, Pagination{Page: input.Page, PerPage: input.PerPage, Total: total, TotalPages: totalPages(total, input.PerPage)}, nil
}

func (s *Service) Get(ctx context.Context, inventoryID int64) (InventoryItem, error) {
	item, err := s.repository.FindByID(ctx, inventoryID)
	if err != nil {
		return InventoryItem{}, err
	}
	return *item, nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, input UpsertInventoryRequest) (InventoryItem, error) {
	input = normalizeInventory(input)
	if err := s.canManage(ctx, actor, input.NurseryID); err != nil {
		return InventoryItem{}, err
	}
	if err := validateInventory(input); err != nil {
		return InventoryItem{}, err
	}
	item, err := s.repository.Create(ctx, actor.UserID, input)
	if err != nil {
		return InventoryItem{}, err
	}
	s.audit(ctx, actor, item.ID, actionInsert, input)
	return *item, nil
}

func (s *Service) Update(ctx context.Context, actor ActorContext, inventoryID int64, input UpsertInventoryRequest) (InventoryItem, error) {
	input = normalizeInventory(input)
	if err := s.canManage(ctx, actor, input.NurseryID); err != nil {
		return InventoryItem{}, err
	}
	if err := validateInventory(input); err != nil {
		return InventoryItem{}, err
	}
	item, err := s.repository.Update(ctx, actor.UserID, inventoryID, input)
	if err != nil {
		return InventoryItem{}, err
	}
	s.audit(ctx, actor, item.ID, actionUpdate, input)
	return *item, nil
}

func (s *Service) Delete(ctx context.Context, actor ActorContext, inventoryID int64) error {
	item, err := s.repository.FindByID(ctx, inventoryID)
	if err != nil {
		return err
	}
	if err := s.canManage(ctx, actor, item.NurseryID); err != nil {
		return err
	}
	if err := s.repository.Delete(ctx, inventoryID); err != nil {
		return err
	}
	s.audit(ctx, actor, inventoryID, actionDelete, map[string]any{"deleted": true})
	return nil
}

func (s *Service) canManage(ctx context.Context, actor ActorContext, nurseryID int64) error {
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

func (s *Service) audit(ctx context.Context, actor ActorContext, entityID int64, action auditlog.Action, newValue any) {
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:     actor.UserID,
		Module:     auditlog.ModuleInventory,
		EntityType: "inventory_item",
		EntityID:   entityID,
		Action:     action,
		NewValue:   newValue,
		IPAddress:  actor.IPAddress,
		DeviceInfo: actor.UserAgent,
	})
}

func normalizeList(input ListInventoryRequest) ListInventoryRequest {
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

func normalizeInventory(input UpsertInventoryRequest) UpsertInventoryRequest {
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "AVAILABLE"
	}
	return input
}

func validateInventory(input UpsertInventoryRequest) error {
	if input.NurseryID <= 0 || input.PlantID <= 0 || input.SizeID <= 0 {
		return ErrInvalidInput
	}
	if input.AvailableQuantity < 0 {
		return ErrInvalidInput
	}
	if !isAllowedStatus(input.Status) {
		return ErrInvalidInput
	}
	return nil
}

func isAllowedStatus(value string) bool {
	switch value {
	case "", "AVAILABLE", "LOW_STOCK", "OUT_OF_STOCK", "RESERVED", "DISCONTINUED":
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

