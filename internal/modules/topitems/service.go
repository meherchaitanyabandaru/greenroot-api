package topitems

import (
	"context"
	"errors"
	"strings"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
)

var (
	ErrForbidden    = apperrs.ErrForbidden
	ErrInvalidInput = apperrs.ErrInvalidInput
	ErrLimitReached = errors.New("a nursery can list at most 20 top items")
	ErrDuplicate    = errors.New("this plant and size is already a top item")
)

type Service struct {
	repository Repository
	auditSvc   *auditlog.Service
}

func NewService(repository Repository, auditSvc *auditlog.Service) *Service {
	return &Service{repository: repository, auditSvc: auditSvc}
}

func (s *Service) List(ctx context.Context, actor ActorContext, input ListRequest) ([]TopItem, Pagination, error) {
	input = normalizeList(input)
	items, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	byNursery := map[int64]*Capabilities{} // one ownership check per nursery, not per row
	for i := range items {
		caps, ok := byNursery[items[i].NurseryID]
		if !ok {
			caps = s.capabilities(ctx, actor, items[i].NurseryID)
			byNursery[items[i].NurseryID] = caps
		}
		items[i].Capabilities = caps
	}
	return items, Pagination{Page: input.Page, PerPage: input.PerPage, Total: total, TotalPages: totalPages(total, input.PerPage)}, nil
}

func (s *Service) Get(ctx context.Context, actor ActorContext, id int64) (TopItem, error) {
	item, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return TopItem{}, err
	}
	item.Capabilities = s.capabilities(ctx, actor, item.NurseryID)
	return *item, nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, input CreateRequest) (TopItem, error) {
	if input.NurseryID <= 0 || !validItem(input.PlantID, input.SizeID, input.UnitPrice, input.DisplayOrder, input.QualityNotes, input.Photos) {
		return TopItem{}, ErrInvalidInput
	}
	if err := s.canManage(ctx, actor, input.NurseryID); err != nil {
		return TopItem{}, err
	}
	item, err := s.repository.Create(ctx, actor.UserID, input)
	if err != nil {
		return TopItem{}, err
	}
	s.audit(ctx, actor, item.ID, actionInsert, input)
	item.Capabilities = &Capabilities{CanEdit: true, CanDelete: true}
	return *item, nil
}

func (s *Service) Update(ctx context.Context, actor ActorContext, id int64, input UpdateRequest) (TopItem, error) {
	if !validItem(input.PlantID, input.SizeID, input.UnitPrice, input.DisplayOrder, input.QualityNotes, input.Photos) {
		return TopItem{}, ErrInvalidInput
	}
	// Permission comes from the item's own nursery, never from the request.
	current, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return TopItem{}, err
	}
	if err := s.canManage(ctx, actor, current.NurseryID); err != nil {
		return TopItem{}, err
	}
	item, err := s.repository.Update(ctx, actor.UserID, id, input)
	if err != nil {
		return TopItem{}, err
	}
	s.audit(ctx, actor, item.ID, actionUpdate, input)
	item.Capabilities = &Capabilities{CanEdit: true, CanDelete: true}
	return *item, nil
}

func (s *Service) Delete(ctx context.Context, actor ActorContext, id int64) error {
	item, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.canManage(ctx, actor, item.NurseryID); err != nil {
		return err
	}
	if err := s.repository.Delete(ctx, id); err != nil {
		return err
	}
	s.audit(ctx, actor, id, actionDelete, map[string]any{"deleted": true})
	return nil
}

// canManage: admins, or the owner or an active manager of that nursery.
func (s *Service) canManage(ctx context.Context, actor ActorContext, nurseryID int64) error {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return nil
	}
	if actor.UserID > 0 && (actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER")) {
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

func (s *Service) capabilities(ctx context.Context, actor ActorContext, nurseryID int64) *Capabilities {
	ok := s.canManage(ctx, actor, nurseryID) == nil
	return &Capabilities{CanEdit: ok, CanDelete: ok}
}

func (s *Service) audit(ctx context.Context, actor ActorContext, entityID int64, action auditlog.Action, newValue any) {
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:     actor.UserID,
		Module:     auditlog.ModuleTopItems,
		EntityType: auditlog.EntityTopItem,
		EntityID:   entityID,
		Action:     action,
		NewValue:   newValue,
		IPAddress:  actor.IPAddress,
		DeviceInfo: actor.UserAgent,
	})
}

func validItem(plantID int64, sizeID int16, price *float64, order *int16, notes *string, photos []string) bool {
	if plantID <= 0 || sizeID <= 0 || (price != nil && *price < 0) || (order != nil && (*order < 0 || *order >= MaxPerNursery)) {
		return false
	}
	if notes != nil && len([]rune(*notes)) > MaxNotesLength {
		return false
	}
	if len(photos) > MaxPhotos {
		return false
	}
	for _, p := range photos {
		if strings.TrimSpace(p) == "" {
			return false
		}
	}
	return true
}

func normalizeList(input ListRequest) ListRequest {
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
	input.SortBy = strings.TrimSpace(input.SortBy)
	input.SortOrder = strings.ToLower(strings.TrimSpace(input.SortOrder))
	if input.SortOrder != "asc" && input.SortOrder != "desc" {
		input.SortOrder = ""
	}
	return input
}

func totalPages(total int64, perPage int) int {
	if total == 0 {
		return 0
	}
	return int((total + int64(perPage) - 1) / int64(perPage))
}
