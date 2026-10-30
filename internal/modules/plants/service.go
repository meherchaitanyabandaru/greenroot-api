package plants

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/redis/go-redis/v9"
)

var (
	ErrForbidden    = apperrs.ErrForbidden
	ErrInvalidInput = apperrs.ErrInvalidInput
	ErrConflict     = apperrs.ErrConflict
	// ErrPlantRemoved: the species exists in the catalog but an admin removed it.
	ErrPlantRemoved = fmt.Errorf("%w: plant removed from the catalog", apperrs.ErrConflict)
)

// Search cache scope for plant searches; one shared scope for all users.
const (
	searchModule      = "plants"
	searchSharedScope = 0
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

func (s *Service) List(ctx context.Context, input ListPlantsRequest) ([]Plant, Pagination, error) {
	input = normalizeListRequest(input)

	// Global Search's "Plants" category (30-minute TTL -- a mostly-static
	// catalog is an excellent cache candidate). Only the search path is
	// cached; plain unfiltered browsing goes straight to Postgres as before.
	if input.Search != "" {
		return s.listSearchCached(ctx, input)
	}

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

type cachedPlantList struct {
	Plants     []Plant    `json:"plants"`
	Pagination Pagination `json:"pagination"`
}

// listSearchCached serves the plants.List search path through Redis. Plants
// have no actor/RBAC scoping at all -- every caller sees the identical
// catalog -- so a single shared cache key per query is correct and safe,
// unlike orders/quotations/market ads, which are actor-personalized and
// must never share a cache entry across users (see searchcache.go).
func (s *Service) listSearchCached(ctx context.Context, input ListPlantsRequest) ([]Plant, Pagination, error) {
	const module = searchModule
	const sharedScope = searchSharedScope
	// Feeds Search Suggestions with real query popularity, independent of
	// whether this particular request is a cache hit or miss.
	redisutil.RecordSearchTerm(ctx, s.redis, nil, module, input.Search)
	gen := redisutil.SearchGeneration(ctx, s.redis, nil, module, sharedScope)
	key := redisutil.SearchCacheKey(module, sharedScope, gen,
		input.Search, fmt.Sprint(input.Page), fmt.Sprint(input.PerPage),
		fmt.Sprint(input.CategoryID), input.PlantType, input.LightRequirement,
		input.WaterRequirement, input.SortBy, input.SortOrder)

	if cached, ok := redisutil.GetCachedSearch[cachedPlantList](ctx, s.redis, nil, module, input.Search, key); ok {
		return cached.Plants, cached.Pagination, nil
	}

	start := time.Now()
	plants, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	pagination := Pagination{
		Page:       input.Page,
		PerPage:    input.PerPage,
		Total:      total,
		TotalPages: totalPages(total, input.PerPage),
	}
	redisutil.SetCachedSearch(ctx, s.redis, nil, module, input.Search, key,
		cachedPlantList{Plants: plants, Pagination: pagination}, redisutil.SearchTTLPlants, time.Since(start))
	return plants, pagination, nil
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
	if err := s.validatePlant(ctx, input); err != nil {
		return Plant{}, err
	}
	plant, err := s.repository.Create(ctx, input)
	if err != nil {
		return Plant{}, err
	}
	s.invalidateSearch(ctx)
	s.audit(ctx, actor, plant.ID, auditlog.ActionCreate,
		fmt.Sprintf("Plant %q created", plant.ScientificName), nil, input)
	return *plant, nil
}

func (s *Service) Update(ctx context.Context, actor ActorContext, plantID int64, input UpdatePlantRequest) (Plant, error) {
	if !canManagePlants(actor) {
		return Plant{}, ErrForbidden
	}
	input = normalizePlantInput(input)
	if err := s.validatePlant(ctx, input); err != nil {
		return Plant{}, err
	}
	old, _ := s.repository.FindByID(ctx, plantID)
	plant, err := s.repository.Update(ctx, plantID, input)
	if err != nil {
		return Plant{}, err
	}
	s.invalidateSearch(ctx)
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
	s.invalidateSearch(ctx)
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
	if input.WfoID != nil {
		v := strings.TrimSpace(*input.WfoID)
		input.WfoID = &v
	}
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

// validatePlant also checks that a wfo_id names a real botanical name.
func (s *Service) validatePlant(ctx context.Context, input CreatePlantRequest) error {
	if err := validatePlantInput(input); err != nil {
		return err
	}
	if input.WfoID == nil || *input.WfoID == "" {
		return nil
	}
	ok, err := s.repository.BotanicalNameExists(ctx, *input.WfoID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidInput
	}
	return nil
}

func (s *Service) invalidateSearch(ctx context.Context) {
	redisutil.BumpSearchGeneration(ctx, s.redis, nil, searchModule, searchSharedScope)
}

// Lookup serves every plant picker: catalog plants first (by name, by any of
// their aliases such as local names, then near spellings), then botanical
// names not in the catalog yet (substring, then near spellings). It reads the
// database directly, not the search cache, so a plant resolved a moment ago
// shows up as a catalog plant immediately. Results are offers: the apps
// never pick one for the user and always allow "Other / not listed".
func (s *Service) Lookup(ctx context.Context, input LookupRequest) ([]LookupResult, error) {
	q := strings.Join(strings.Fields(input.Query), " ")
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	results := make([]LookupResult, 0, limit)
	if q == "" {
		return results, nil
	}
	matches, err := s.repository.LookupCatalog(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	if len(matches) > 0 {
		ids := make([]int64, len(matches))
		for i, m := range matches {
			ids[i] = m.PlantID
		}
		plants, _, err := s.repository.List(ctx, ListPlantsRequest{Page: 1, PerPage: len(ids), IDs: ids, SortBy: "id"})
		if err != nil {
			return nil, err
		}
		byID := make(map[int64]*Plant, len(plants))
		for i := range plants {
			byID[plants[i].ID] = &plants[i]
		}
		for _, m := range matches {
			if p := byID[m.PlantID]; p != nil {
				results = append(results, LookupResult{Plant: p, MatchType: m.MatchType, MatchedName: m.MatchedName})
			}
		}
	}
	if len([]rune(q)) < minBotanicalQuery || len(results) >= limit {
		return results, nil
	}
	seen := map[string]bool{}
	add := func(names []BotanicalName, matchType string) {
		for i := range names {
			if k := botanicalKey(names[i]); !seen[k] && len(results) < limit {
				seen[k] = true
				results = append(results, LookupResult{Name: &names[i], MatchType: matchType})
			}
		}
	}
	names, err := s.repository.LookupBotanical(ctx, q, limit-len(results))
	if err != nil {
		return nil, err
	}
	add(names, "BOTANICAL")
	if len(results) < limit && len([]rune(q)) >= minFuzzyQuery {
		fuzzy, err := s.repository.LookupBotanicalFuzzy(ctx, q, limit-len(results))
		if err != nil {
			return nil, err
		}
		add(fuzzy, "BOTANICAL_FUZZY")
	}
	return results, nil
}

// Resolve returns the catalog plant for a botanical name, adding it to the
// catalog on first use. Any signed-in user may do this: the name comes from
// the curated WFO list, so it is not free-text master data.
func (s *Service) Resolve(ctx context.Context, actor ActorContext, input ResolvePlantRequest) (Plant, error) {
	wfoID := strings.TrimSpace(input.WfoID)
	if wfoID == "" {
		return Plant{}, ErrInvalidInput
	}
	plantID, created, err := s.repository.ResolveBotanical(ctx, wfoID)
	if err != nil {
		return Plant{}, err
	}
	plant, err := s.repository.FindByID(ctx, plantID)
	if err != nil {
		return Plant{}, err
	}
	if created {
		s.invalidateSearch(ctx)
		s.audit(ctx, actor, plant.ID, auditlog.ActionCreate,
			fmt.Sprintf("Plant %q added to the catalog from botanical name %s", plant.ScientificName, wfoID), nil, map[string]any{"wfo_id": wfoID})
	}
	return *plant, nil
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
