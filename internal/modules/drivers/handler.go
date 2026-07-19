package drivers

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
	drivers, pagination, err := h.service.List(r.Context(), actor, listRequest(r))
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, DriversResponse{Drivers: drivers, Pagination: pagination})
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
	driver, err := h.service.Get(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, DriverResponse{Driver: driver})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req DriverRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	driver, err := h.service.Create(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, DriverResponse{Driver: driver})
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
	var req DriverRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	driver, err := h.service.Update(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, DriverResponse{Driver: driver})
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
	response.OK(w, MessageResponse{Message: "Driver deactivated successfully"})
}

// Apply allows a logged-in user to self-register as a driver (V1 flow).
func (h *Handler) Apply(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req ApplyDriverRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	driver, err := h.service.Apply(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, DriverResponse{Driver: driver})
}

// GetMine returns the current user's driver profile.
func (h *Handler) GetMine(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	driver, err := h.service.GetMine(r.Context(), actor)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, DriverResponse{Driver: driver})
}

// ApproveDriver approves a driver profile (admin only).
func (h *Handler) ApproveDriver(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	driverUserID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	driver, err := h.service.Approve(r.Context(), actor, driverUserID)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, DriverResponse{Driver: driver})
}

func (h *Handler) CreateLocation(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req LocationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	location, err := h.service.CreateLocation(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, LocationResponse{Location: location})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return actor.AsActorContext(), true
}

func listRequest(r *http.Request) ListDriversRequest {
	query := r.URL.Query()
	return ListDriversRequest{
		Page:      intQuery(query.Get("page")),
		PerPage:   intQuery(query.Get("per_page")),
		Status:    query.Get("status"),
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
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to access driver")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "driver not found")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid driver input")
	case errors.Is(err, ErrDuplicate):
		response.Error(w, http.StatusConflict, "duplicate_driver", "driver already exists for this user or license number")
	case errors.Is(err, ErrOwnerCannotBeDriver):
		response.Error(w, http.StatusConflict, "owner_conflict", "nursery owners cannot register as a driver")
	default:
		response.Error(w, http.StatusInternalServerError, "drivers_error", "driver request failed")
	}
}
