package plants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrForbidden    = errors.New("forbidden")
	ErrInvalidInput = errors.New("invalid input")
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
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
	s.audit(ctx, actor, plant.ID, actionInsert, input)
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
	plant, err := s.repository.Update(ctx, plantID, input)
	if err != nil {
		return Plant{}, err
	}
	s.audit(ctx, actor, plant.ID, actionUpdate, input)
	return *plant, nil
}

func (s *Service) Delete(ctx context.Context, actor ActorContext, plantID int64) error {
	if !canManagePlants(actor) {
		return ErrForbidden
	}
	if err := s.repository.Delete(ctx, plantID); err != nil {
		return err
	}
	s.audit(ctx, actor, plantID, actionDelete, map[string]any{"is_active": false})
	return nil
}

func (s *Service) ListCategories(ctx context.Context) ([]Category, error) {
	return s.repository.ListCategories(ctx)
}

func (s *Service) CreateCategory(ctx context.Context, actor ActorContext, input CreateCategoryRequest) (Category, error) {
	if !hasRole(actor, "ADMIN") {
		return Category{}, ErrForbidden
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Category{}, ErrInvalidInput
	}
	return s.repository.CreateCategory(ctx, name)
}

func (s *Service) UpdateCategory(ctx context.Context, actor ActorContext, categoryID int64, input UpdateCategoryRequest) (Category, error) {
	if !hasRole(actor, "ADMIN") {
		return Category{}, ErrForbidden
	}
	return s.repository.UpdateCategory(ctx, categoryID, input)
}

func (s *Service) DeleteCategory(ctx context.Context, actor ActorContext, categoryID int64) error {
	if !hasRole(actor, "ADMIN") {
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
	s.audit(ctx, actor, plantID, actionUpdate, map[string]any{"image_id": image.ID, "image_url": image.ImageURL})
	return *image, nil
}

func (s *Service) GetCareGuide(ctx context.Context, plantID int64) (CareGuide, error) {
	guide, err := s.repository.GetCareGuide(ctx, plantID)
	if err != nil {
		return CareGuide{}, err
	}
	return *guide, nil
}

func (s *Service) audit(ctx context.Context, actor ActorContext, plantID int64, action string, data any) {
	_ = s.repository.CreateAuditLog(ctx, CreateAuditInput{
		TableName: "plants",
		RecordID:  plantID,
		Action:    action,
		ChangedBy: actor.UserID,
		SourceIP:  actor.IPAddress,
		UserAgent: actor.UserAgent,
		NewJSON:   mustJSON(data),
		At:        time.Now(),
	})
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
	return hasRole(actor, "ADMIN") || hasRole(actor, "NURSERY_OWNER")
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

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(data)
}
