package plants

import (
	"context"
	"fmt"
	"strings"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
)

var (
	ErrForbidden    = apperrs.ErrForbidden
	ErrInvalidInput = apperrs.ErrInvalidInput
)

type Service struct {
	repository Repository
	auditSvc   *auditlog.Service
}

func NewService(repository Repository, auditSvc *auditlog.Service) *Service {
	return &Service{repository: repository, auditSvc: auditSvc}
}

func (s *Service) List(ctx context.Context, input ListPlantsRequest) ([]Plant, Pagination, error) {
	input = normalizeListRequest(input)
	plants, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	return plants, Pagination{
		Page:       input.Page,
		PerPage:    input.PerPage,
		Total:      total,
		TotalPages: totalPages(total, input.PerPage),
	}, nil
}

func (s *Service) GetNamesByLanguage(ctx context.Context, plantIDs []int64, langCode string) (map[int64]string, error) {
	return s.repository.GetNamesByLanguage(ctx, plantIDs, langCode)
}

func (s *Service) Get(ctx context.Context, plantID int64) (Plant, error) {
	plant, err := s.repository.FindByID(ctx, plantID)
	if err != nil {
		return Plant{}, err
	}
	return *plant, nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, input CreatePlantRequest) (Plant, error) {
	if !canManagePlants(actor) {
		return Plant{}, ErrForbidden
	}
	input = normalizePlantInput(input)
	if err := validatePlantInput(input); err != nil {
		return Plant{}, err
	}
	plant, err := s.repository.Create(ctx, input)
	if err != nil {
		return Plant{}, err
	}
	s.audit(ctx, actor, plant.ID, auditlog.ActionCreate,
		fmt.Sprintf("Plant %q created", plant.ScientificName), nil, input)
	return *plant, nil
}

func (s *Service) Update(ctx context.Context, actor ActorContext, plantID int64, input UpdatePlantRequest) (Plant, error) {
	if !canManagePlants(actor) {
		return Plant{}, ErrForbidden
	}
	input = normalizePlantInput(input)
	if err := validatePlantInput(input); err != nil {
		return Plant{}, err
	}
	old, _ := s.repository.FindByID(ctx, plantID)
	plant, err := s.repository.Update(ctx, plantID, input)
	if err != nil {
		return Plant{}, err
	}
	s.audit(ctx, actor, plant.ID, auditlog.ActionUpdate,
		fmt.Sprintf("Plant %q updated", plant.ScientificName), old, input)
	return *plant, nil
}

func (s *Service) Delete(ctx context.Context, actor ActorContext, plantID int64) error {
	if !canManagePlants(actor) {
		return ErrForbidden
	}
	old, _ := s.repository.FindByID(ctx, plantID)
	if err := s.repository.Delete(ctx, plantID); err != nil {
		return err
	}
	s.audit(ctx, actor, plantID, auditlog.ActionDelete,
		fmt.Sprintf("Plant %q deleted", nameOrID(old, plantID)), old, map[string]any{"is_active": false})
	return nil
}

func (s *Service) ListSizes(ctx context.Context) ([]PlantSize, error) {
	return s.repository.ListSizes(ctx)
}

func (s *Service) ListCategories(ctx context.Context) ([]Category, error) {
	return s.repository.ListCategories(ctx)
}

func (s *Service) CreateCategory(ctx context.Context, actor ActorContext, input CreateCategoryRequest) (Category, error) {
	if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		return Category{}, ErrForbidden
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Category{}, ErrInvalidInput
	}
	return s.repository.CreateCategory(ctx, name)
}

func (s *Service) UpdateCategory(ctx context.Context, actor ActorContext, categoryID int64, input UpdateCategoryRequest) (Category, error) {
	if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		return Category{}, ErrForbidden
	}
	return s.repository.UpdateCategory(ctx, categoryID, input)
}

func (s *Service) DeleteCategory(ctx context.Context, actor ActorContext, categoryID int64) error {
	if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		return ErrForbidden
	}
	return s.repository.DeleteCategory(ctx, categoryID)
}

func (s *Service) CreateImage(ctx context.Context, actor ActorContext, plantID int64, input CreateImageRequest) (Image, error) {
	if !canManagePlants(actor) {
		return Image{}, ErrForbidden
	}
	input.ImageURL = strings.TrimSpace(input.ImageURL)
	if input.ImageURL == "" {
		return Image{}, ErrInvalidInput
	}
	image, err := s.repository.CreateImage(ctx, plantID, input)
	if err != nil {
		return Image{}, err
	}
	s.audit(ctx, actor, plantID, auditlog.ActionUpdate,
		"Plant image added", nil, map[string]any{"image_id": image.ID, "image_url": image.ImageURL})
	return *image, nil
}

func (s *Service) GetCareGuide(ctx context.Context, plantID int64) (CareGuide, error) {
	guide, err := s.repository.GetCareGuide(ctx, plantID)
	if err != nil {
		return CareGuide{}, err
	}
	return *guide, nil
}

func (s *Service) audit(ctx context.Context, actor ActorContext, entityID int64, action auditlog.Action, description string, oldValue, newValue any) {
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:      actor.UserID,
		Module:      auditlog.ModulePlants,
		EntityType:  auditlog.EntityPlant,
		EntityID:    entityID,
		Action:      action,
		Description: description,
		OldValue:    oldValue,
		NewValue:    newValue,
		IPAddress:   actor.IPAddress,
		DeviceInfo:  actor.UserAgent,
	})
}

func nameOrID(p *Plant, id int64) string {
	if p != nil && p.ScientificName != "" {
		return p.ScientificName
	}
	return fmt.Sprintf("#%d", id)
}

func normalizeListRequest(input ListPlantsRequest) ListPlantsRequest {
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
	input.PlantType = strings.ToUpper(strings.TrimSpace(input.PlantType))
	input.LightRequirement = strings.ToUpper(strings.TrimSpace(input.LightRequirement))
	input.WaterRequirement = strings.ToUpper(strings.TrimSpace(input.WaterRequirement))
	input.SortBy = strings.TrimSpace(input.SortBy)
	input.SortOrder = strings.ToLower(strings.TrimSpace(input.SortOrder))
	if input.SortOrder != "asc" && input.SortOrder != "desc" {
		input.SortOrder = "desc"
	}
	return input
}

func normalizePlantInput(input CreatePlantRequest) CreatePlantRequest {
	input.ScientificName = strings.TrimSpace(input.ScientificName)
	upperOptional(input.PlantType)
	upperOptional(input.LightRequirement)
	upperOptional(input.WaterRequirement)
	return input
}

func validatePlantInput(input CreatePlantRequest) error {
	if input.ScientificName == "" {
		return ErrInvalidInput
	}
	for _, categoryID := range input.CategoryIDs {
		if categoryID <= 0 {
			return ErrInvalidInput
		}
	}
	return nil
}

func canManagePlants(actor ActorContext) bool {
	return actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN")
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

