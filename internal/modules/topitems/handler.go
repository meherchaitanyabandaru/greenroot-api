package topitems

import (
	"encoding/json"
	"errors"
	"math"
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
//	@Summary	List top items (all nurseries; filter by nursery_id, plant_id, size_id, search)
//	@Tags		TopItems
//	@Security	BearerAuth
//	@Success	200	{object}	ListResponse
//	@Router		/api/v1/top-items [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, listRequest(r))
}

func (h *Handler) ListByNursery(w http.ResponseWriter, r *http.Request) {
	nurseryID, ok := pathID(w, r, "nurseryId")
	if !ok {
		return
	}
	req := listRequest(r)
	req.NurseryID = nurseryID
	h.list(w, r, req)
}

func (h *Handler) ListByPlant(w http.ResponseWriter, r *http.Request) {
	plantID, ok := pathID(w, r, "plantId")
	if !ok {
		return
	}
	req := listRequest(r)
	req.PlantID = plantID
	h.list(w, r, req)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, req ListRequest) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	items, pagination, err := h.service.List(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, ListResponse{TopItems: items, Pagination: pagination})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.Get(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, ItemResponse{TopItem: item})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.Create(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, ItemResponse{TopItem: item})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req UpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.Update(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, ItemResponse{TopItem: item})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), actor, id); err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "Top item removed"})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return actor.AsActorContext(), true
}

func listRequest(r *http.Request) ListRequest {
	q := r.URL.Query()
	return ListRequest{
		Page:      intQuery(q.Get("page")),
		PerPage:   intQuery(q.Get("per_page")),
		Search:    q.Get("search"),
		NurseryID: int64Query(q.Get("nursery_id")),
		PlantID:   int64Query(q.Get("plant_id")),
		SizeID:    int16Query(q.Get("size_id")),
		SortBy:    q.Get("sort_by"),
		SortOrder: q.Get("sort_order"),
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

// int16Query parses a smallint filter; out-of-range or invalid values are ignored (0).
func int16Query(v string) int16 {
	n := intQuery(v)
	if n < 0 || n > math.MaxInt16 {
		return 0
	}
	return int16(n)
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "only the nursery owner or an admin can change top items")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "top item or nursery not found")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "plant_id and size_id are required; unit_price must be 0 or more; display_order 0–19")
	case errors.Is(err, ErrLimitReached):
		response.Error(w, http.StatusConflict, "top_items_limit", ErrLimitReached.Error())
	case errors.Is(err, ErrDuplicate):
		response.Error(w, http.StatusConflict, "already_listed", ErrDuplicate.Error())
	case errors.Is(err, ErrConflict):
		response.Error(w, http.StatusConflict, "version_conflict", "top item was changed by someone else; reload and retry")
	default:
		response.Error(w, http.StatusInternalServerError, "top_items_error", "top items request failed")
	}
}
