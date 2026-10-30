package vehicles

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
	if !response.DecodeJSON(w, r, &req) {
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
	if !response.DecodeJSON(w, r, &req) {
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
	return actor.AsActorContext(), true
}

func listRequest(r *http.Request) ListVehiclesRequest {
	query := r.URL.Query()
	return ListVehiclesRequest{
		Page:      intQuery(query.Get("page")),
		PerPage:   intQuery(query.Get("per_page")),
		Status:    query.Get("status"),
		Type:      query.Get("vehicle_type"),
		OwnerID:   int64(intQuery(query.Get("owner_id"))),
		Approval:  query.Get("approval_status"),
		Search:    query.Get("search"),
		SortBy:    query.Get("sort_by"),
		SortOrder: query.Get("sort_order"),
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
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to manage vehicles")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "vehicle not found")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid vehicle input")
	case errors.Is(err, ErrDuplicate):
		response.Error(w, http.StatusConflict, "duplicate_vehicle", "vehicle already exists with this vehicle number")
	case errors.Is(err, ErrVehicleAssigned):
		response.Error(w, http.StatusConflict, "vehicle_assigned", "vehicle is assigned to an active approved driver")
	case errors.Is(err, ErrVehicleOnTrip):
		response.Error(w, http.StatusConflict, "vehicle_on_active_trip", "This lorry is on an active trip. Wait until the trip ends.")
	case errors.Is(err, ErrInvalidStatus):
		response.Error(w, http.StatusConflict, "invalid_status_transition", "this status change is not allowed")
	case errors.Is(err, ErrDuplicateOwner):
		response.Error(w, http.StatusConflict, "duplicate_owner", "an owner with this mobile number already exists")
	case errors.Is(err, ErrInvalidMobile):
		response.Error(w, http.StatusBadRequest, "invalid_mobile", "enter a valid 10-digit mobile number")
	default:
		response.Error(w, http.StatusInternalServerError, "vehicles_error", "vehicle request failed")
	}
}

func (h *Handler) ListOwners(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	owners, pagination, err := h.service.ListOwners(r.Context(), actor, ListOwnersRequest{
		Page: intQuery(q.Get("page")), PerPage: intQuery(q.Get("per_page")), Search: q.Get("search")})
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, OwnersResponse{Owners: owners, Pagination: pagination})
}

func (h *Handler) GetOwner(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	owner, err := h.service.GetOwner(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, OwnerResponse{Owner: owner})
}

// SaveOwner creates (POST /vehicle-owners) or updates (PUT /vehicle-owners/{id}) an owner.
func (h *Handler) SaveOwner(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var id int64
	if chi.URLParam(r, "id") != "" {
		if id, ok = pathID(w, r, "id"); !ok {
			return
		}
	}
	var req OwnerRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	owner, err := h.service.SaveOwner(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusOK
	if id == 0 {
		status = http.StatusCreated
	}
	response.JSON(w, status, OwnerResponse{Owner: owner})
}

func (h *Handler) SetApproval(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req ApprovalRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	vehicle, err := h.service.SetApproval(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, VehicleResponse{Vehicle: vehicle})
}

func (h *Handler) ListLinks(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	links, pagination, err := h.service.ListLinks(r.Context(), actor, ListLinksRequest{
		Page: intQuery(q.Get("page")), PerPage: intQuery(q.Get("per_page")), Status: q.Get("status"),
		VehicleID: int64(intQuery(q.Get("vehicle_id"))), DriverUserID: int64(intQuery(q.Get("driver_user_id"))),
		Search: q.Get("search")})
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, LinksResponse{Links: links, Pagination: pagination})
}

func (h *Handler) SetLinkStatus(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req LinkStatusRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	link, err := h.service.SetLinkStatus(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, LinkResponse{Link: link})
}
