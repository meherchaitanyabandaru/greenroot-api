package drivers

import (
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
	if !response.DecodeJSON(w, r, &req) {
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
	if !response.DecodeJSON(w, r, &req) {
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
	if !response.DecodeJSON(w, r, &req) {
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

// StopDriving ends the caller's driving (DELETE /drivers/me).
func (h *Handler) StopDriving(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	if err := h.service.StopDriving(r.Context(), actor); err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "You have stopped driving"})
}

// MyVehicles lists the caller's lorry links and which can take a trip now.
func (h *Handler) MyVehicles(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	vehicles, err := h.service.MyVehicles(r.Context(), actor)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, MyVehiclesResponse{Vehicles: vehicles})
}

// AddVehicle asks GreenRoot to link the caller to a lorry (POST /drivers/me/vehicles).
func (h *Handler) AddVehicle(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req AddVehicleRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	vehicle, err := h.service.AddVehicle(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, MyVehicleResponse{Vehicle: vehicle})
}

// RemoveVehicle ends one of the caller's lorry links.
func (h *Handler) RemoveVehicle(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	linkID, ok := pathID(w, r, "linkId")
	if !ok {
		return
	}
	if err := h.service.RemoveVehicle(r.Context(), actor, linkID); err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "Lorry removed"})
}

// ApproveDriver approves a driver profile (admin only).
func (h *Handler) ApproveDriver(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	driverID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	driver, err := h.service.Approve(r.Context(), actor, driverID)
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
	if !response.DecodeJSON(w, r, &req) {
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
		Page:           intQuery(query.Get("page")),
		PerPage:        intQuery(query.Get("per_page")),
		Status:         query.Get("status"),
		ApprovalStatus: query.Get("approval_status"),
		Search:         query.Get("search"),
		SortBy:         query.Get("sort_by"),
		SortOrder:      query.Get("sort_order"),
	}
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
	case errors.Is(err, ErrActiveTrip):
		response.Error(w, http.StatusConflict, "active_trip", "finish or hand over your active trip before you stop driving")
	case errors.Is(err, ErrInvalidLicencePhoto):
		response.Error(w, http.StatusBadRequest, "invalid_licence_photo", "upload the licence photo again")
	case errors.Is(err, ErrVehicleLinked):
		response.Error(w, http.StatusConflict, "vehicle_already_linked", "You have already added this lorry.")
	case errors.Is(err, ErrVehicleOnTrip):
		response.Error(w, http.StatusConflict, "vehicle_on_active_trip", "Finish the trip with this lorry before you remove it.")
	case errors.Is(err, ErrLicenceExpired):
		response.Error(w, http.StatusBadRequest, "licence_expired", "Your licence has expired. Renew it before you register.")
	case errors.Is(err, ErrInvalidOwnerPhone):
		response.Error(w, http.StatusBadRequest, "invalid_owner_mobile", "Enter the owner's 10-digit mobile number, or leave it empty.")
	case errors.Is(err, ErrOwnerCannotBeDriver):
		response.Error(w, http.StatusConflict, "owner_conflict", "nursery owners cannot register as a driver")
	default:
		response.Error(w, http.StatusInternalServerError, "drivers_error", "driver request failed")
	}
}
