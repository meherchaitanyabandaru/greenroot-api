package plants

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Handler struct {
	service *Service
	jwt     *jwtplatform.Service
}

func NewHandler(service *Service, jwt *jwtplatform.Service) *Handler {
	return &Handler{service: service, jwt: jwt}
}

// List godoc
//
//	@Summary	List plants
//	@Tags		Plants
//	@Param		page				query	int		false	"Page number"
//	@Param		per_page			query	int		false	"Items per page"
//	@Param		search				query	string	false	"Search text"
//	@Param		category_id			query	int		false	"Category id"
//	@Param		plant_type			query	string	false	"Plant type"
//	@Param		light_requirement	query	string	false	"Light requirement"
//	@Param		water_requirement	query	string	false	"Water requirement"
//	@Success	200					{object}	PlantsResponse
//	@Router		/api/v1/plants [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	plants, pagination, err := h.service.List(r.Context(), listPlantsRequest(r))
	if err != nil {
		writePlantsError(w, err)
		return
	}
	response.OK(w, PlantsResponse{Plants: plants, Pagination: pagination})
}

// Get godoc
//
//	@Summary	Get plant
//	@Tags		Plants
//	@Success	200	{object}	PlantResponse
//	@Router		/api/v1/plants/{id} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	plantID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	plant, err := h.service.Get(r.Context(), plantID)
	if err != nil {
		writePlantsError(w, err)
		return
	}
	response.OK(w, PlantResponse{Plant: plant})
}

// Create godoc
//
//	@Summary	Create plant
//	@Tags		Plants
//	@Security	BearerAuth
//	@Param		request	body		CreatePlantRequest	true	"Plant"
//	@Success	201		{object}	PlantResponse
//	@Router		/api/v1/plants [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CreatePlantRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	plant, err := h.service.Create(r.Context(), actor, req)
	if err != nil {
		writePlantsError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, PlantResponse{Plant: plant})
}

// Update godoc
//
//	@Summary	Update plant
//	@Tags		Plants
//	@Security	BearerAuth
//	@Param		request	body		UpdatePlantRequest	true	"Plant"
//	@Success	200		{object}	PlantResponse
//	@Router		/api/v1/plants/{id} [put]
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	plantID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req UpdatePlantRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	plant, err := h.service.Update(r.Context(), actor, plantID, req)
	if err != nil {
		writePlantsError(w, err)
		return
	}
	response.OK(w, PlantResponse{Plant: plant})
}

// Delete godoc
//
//	@Summary	Delete plant
//	@Tags		Plants
//	@Security	BearerAuth
//	@Success	200	{object}	DeletePlantResponse
//	@Router		/api/v1/plants/{id} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	plantID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), actor, plantID); err != nil {
		writePlantsError(w, err)
		return
	}
	response.OK(w, DeletePlantResponse{Message: "Plant deleted successfully"})
}

func (h *Handler) Sizes(w http.ResponseWriter, r *http.Request) {
	sizes, err := h.service.ListSizes(r.Context())
	if err != nil {
		writePlantsError(w, err)
		return
	}
	response.OK(w, SizesResponse{Sizes: sizes})
}

func (h *Handler) Categories(w http.ResponseWriter, r *http.Request) {
	categories, err := h.service.ListCategories(r.Context())
	if err != nil {
		writePlantsError(w, err)
		return
	}
	response.OK(w, CategoriesResponse{Categories: categories})
}

func (h *Handler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CreateCategoryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	category, err := h.service.CreateCategory(r.Context(), actor, req)
	if err != nil {
		writePlantsError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, CategoryResponse{Category: category})
}

func (h *Handler) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	categoryID, ok := pathID(w, r, "categoryId")
	if !ok {
		return
	}
	var req UpdateCategoryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	category, err := h.service.UpdateCategory(r.Context(), actor, categoryID, req)
	if err != nil {
		writePlantsError(w, err)
		return
	}
	response.OK(w, CategoryResponse{Category: category})
}

func (h *Handler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	categoryID, ok := pathID(w, r, "categoryId")
	if !ok {
		return
	}
	if err := h.service.DeleteCategory(r.Context(), actor, categoryID); err != nil {
		writePlantsError(w, err)
		return
	}
	response.OK(w, map[string]string{"message": "Category deactivated"})
}

func (h *Handler) CreateImage(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	plantID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req CreateImageRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	image, err := h.service.CreateImage(r.Context(), actor, plantID, req)
	if err != nil {
		writePlantsError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, ImageResponse{Image: image})
}

func (h *Handler) CareGuide(w http.ResponseWriter, r *http.Request) {
	plantID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	guide, err := h.service.GetCareGuide(r.Context(), plantID)
	if err != nil {
		writePlantsError(w, err)
		return
	}
	response.OK(w, CareGuideResponse{CareGuide: guide})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return ActorContext{UserID: actor.UserID, Roles: actor.Roles, IPAddress: actor.IPAddress, UserAgent: actor.UserAgent}, true
}

func listPlantsRequest(r *http.Request) ListPlantsRequest {
	query := r.URL.Query()
	return ListPlantsRequest{
		Page:             intQuery(query.Get("page")),
		PerPage:          intQuery(query.Get("per_page")),
		Search:           query.Get("search"),
		CategoryID:       int64Query(query.Get("category_id")),
		PlantType:        query.Get("plant_type"),
		LightRequirement: query.Get("light_requirement"),
		WaterRequirement: query.Get("water_requirement"),
		SortBy:           query.Get("sort_by"),
		SortOrder:        query.Get("sort_order"),
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, key), 10, 64)
	if err != nil || id <= 0 {
		response.Error(w, http.StatusBadRequest, "invalid_id", "invalid id")
		return 0, false
	}
	return id, true
}

func intQuery(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func int64Query(value string) int64 {
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func writePlantsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to manage plants")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "plant resource not found")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid plant input")
	default:
		response.Error(w, http.StatusInternalServerError, "plants_error", "plants request failed")
	}
}
