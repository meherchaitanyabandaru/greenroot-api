package vehicles

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

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	vehicles, pagination, err := h.service.List(r.Context(), actor, listRequest(r))
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, VehiclesResponse{Vehicles: vehicles, Pagination: pagination})
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
	vehicle, err := h.service.Get(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, VehicleResponse{Vehicle: vehicle})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req VehicleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	vehicle, err := h.service.Create(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, VehicleResponse{Vehicle: vehicle})
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
	var req VehicleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	vehicle, err := h.service.Update(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, VehicleResponse{Vehicle: vehicle})
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
	response.OK(w, MessageResponse{Message: "Vehicle retired successfully"})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return ActorContext{UserID: actor.UserID, Roles: actor.Roles, IPAddress: actor.IPAddress, UserAgent: actor.UserAgent}, true
}

func listRequest(r *http.Request) ListVehiclesRequest {
	query := r.URL.Query()
	return ListVehiclesRequest{
		Page:      intQuery(query.Get("page")),
		PerPage:   intQuery(query.Get("per_page")),
		Status:    query.Get("status"),
		Type:      query.Get("vehicle_type"),
		Search:    query.Get("search"),
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

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to manage vehicles")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "vehicle not found")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid vehicle input")
	case errors.Is(err, ErrDuplicate):
		response.Error(w, http.StatusConflict, "duplicate_vehicle", "vehicle already exists with this vehicle number")
	case errors.Is(err, ErrVehicleAssigned):
		response.Error(w, http.StatusConflict, "vehicle_assigned", "vehicle is assigned to an active approved driver")
	default:
		response.Error(w, http.StatusInternalServerError, "vehicles_error", "vehicle request failed")
	}
}
