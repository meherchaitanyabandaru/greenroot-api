package inventory

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
//	@Summary	List inventory
//	@Tags		Inventory
//	@Success	200	{object}	InventoryListResponse
//	@Router		/api/v1/inventory [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	items, pagination, err := h.service.List(r.Context(), listRequest(r))
	if err != nil {
		writeInventoryError(w, err)
		return
	}
	response.OK(w, InventoryListResponse{Inventory: items, Pagination: pagination})
}

func (h *Handler) ListByNursery(w http.ResponseWriter, r *http.Request) {
	nurseryID, ok := pathID(w, r, "nurseryId")
	if !ok {
		return
	}
	req := listRequest(r)
	req.NurseryID = nurseryID
	items, pagination, err := h.service.List(r.Context(), req)
	if err != nil {
		writeInventoryError(w, err)
		return
	}
	response.OK(w, InventoryListResponse{Inventory: items, Pagination: pagination})
}

func (h *Handler) ListByPlant(w http.ResponseWriter, r *http.Request) {
	plantID, ok := pathID(w, r, "plantId")
	if !ok {
		return
	}
	req := listRequest(r)
	req.PlantID = plantID
	items, pagination, err := h.service.List(r.Context(), req)
	if err != nil {
		writeInventoryError(w, err)
		return
	}
	response.OK(w, InventoryListResponse{Inventory: items, Pagination: pagination})
}

// Get godoc
//
//	@Summary	Get inventory item
//	@Tags		Inventory
//	@Success	200	{object}	InventoryResponse
//	@Router		/api/v1/inventory/{id} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	inventoryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.Get(r.Context(), inventoryID)
	if err != nil {
		writeInventoryError(w, err)
		return
	}
	response.OK(w, InventoryResponse{Inventory: item})
}

// Create godoc
//
//	@Summary	Create inventory item
//	@Tags		Inventory
//	@Security	BearerAuth
//	@Success	201	{object}	InventoryResponse
//	@Router		/api/v1/inventory [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req UpsertInventoryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.Create(r.Context(), actor, req)
	if err != nil {
		writeInventoryError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, InventoryResponse{Inventory: item})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	inventoryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req UpsertInventoryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.Update(r.Context(), actor, inventoryID, req)
	if err != nil {
		writeInventoryError(w, err)
		return
	}
	response.OK(w, InventoryResponse{Inventory: item})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	inventoryID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), actor, inventoryID); err != nil {
		writeInventoryError(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "Inventory item deleted successfully"})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return ActorContext{UserID: actor.UserID, Roles: actor.Roles, IPAddress: actor.IPAddress, UserAgent: actor.UserAgent}, true
}

func listRequest(r *http.Request) ListInventoryRequest {
	query := r.URL.Query()
	return ListInventoryRequest{
		Page:      intQuery(query.Get("page")),
		PerPage:   intQuery(query.Get("per_page")),
		Search:    query.Get("search"),
		NurseryID: int64Query(query.Get("nursery_id")),
		PlantID:   int64Query(query.Get("plant_id")),
		SizeID:    int16(intQuery(query.Get("size_id"))),
		Status:    query.Get("inventory_status"),
		SortBy:    query.Get("sort_by"),
		SortOrder: query.Get("sort_order"),
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

func writeInventoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to manage inventory")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "inventory resource not found")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid inventory input")
	default:
		response.Error(w, http.StatusInternalServerError, "inventory_error", "inventory request failed")
	}
}
