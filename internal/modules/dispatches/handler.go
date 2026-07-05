package dispatches

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
	dispatches, pagination, err := h.service.List(r.Context(), actor, listRequest(r))
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, DispatchesResponse{Dispatches: dispatches, Pagination: pagination})
}

func (h *Handler) ListByOrder(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orderID, ok := pathID(w, r, "orderId")
	if !ok {
		return
	}
	req := listRequest(r)
	req.OrderID = orderID
	dispatches, pagination, err := h.service.List(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, DispatchesResponse{Dispatches: dispatches, Pagination: pagination})
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
	dispatch, err := h.service.Get(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, DispatchResponse{Dispatch: dispatch})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CreateDispatchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	dispatch, err := h.service.Create(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, DispatchResponse{Dispatch: dispatch})
}

func (h *Handler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req UpdateStatusRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	dispatch, err := h.service.UpdateStatus(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, DispatchResponse{Dispatch: dispatch})
}

func (h *Handler) CreateItem(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req DispatchItemRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.CreateItem(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, DispatchItemResponse{Item: item})
}

// GetByCode looks up a dispatch by its dispatch_code — used by drivers in the Join Trip flow.
func (h *Handler) GetByCode(w http.ResponseWriter, r *http.Request) {
	_, ok := h.actor(w, r)
	if !ok {
		return
	}
	code := chi.URLParam(r, "code")
	if code == "" {
		response.Error(w, http.StatusBadRequest, "invalid_input", "dispatch code is required")
		return
	}
	dispatch, err := h.service.GetByCode(r.Context(), code)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, DispatchResponse{Dispatch: dispatch})
}

// Accept links the authenticated driver to a dispatch (driver accepts the trip).
func (h *Handler) Accept(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	dispatch, err := h.service.AcceptDispatch(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, DispatchResponse{Dispatch: dispatch})
}

// CreateTripEvent records a trip event for a dispatch (driver only).
func (h *Handler) CreateTripEvent(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req CreateTripEventRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	event, err := h.service.CreateTripEvent(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, TripEventResponse{Event: event})
}

// PublicTracking returns dispatch info via public UUID (no auth required).
func (h *Handler) PublicTracking(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		response.Error(w, http.StatusBadRequest, "invalid_uuid", "missing tracking uuid")
		return
	}
	dispatch, err := h.service.GetPublicTracking(r.Context(), uuid)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, PublicTrackingResponse{Dispatch: dispatch})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return ActorContext{UserID: actor.UserID, Roles: actor.Roles, IPAddress: actor.IPAddress, UserAgent: actor.UserAgent}, true
}

func listRequest(r *http.Request) ListDispatchesRequest {
	query := r.URL.Query()
	return ListDispatchesRequest{
		Page:      intQuery(query.Get("page")),
		PerPage:   intQuery(query.Get("per_page")),
		NurseryID: int64Query(query.Get("nursery_id")),
		Buying:    query.Get("buying") == "true" || query.Get("buying") == "1",
		Status:    query.Get("dispatch_status"),
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

func intQuery(value string) int     { parsed, _ := strconv.Atoi(value); return parsed }
func int64Query(value string) int64 { parsed, _ := strconv.ParseInt(value, 10, 64); return parsed }

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to access dispatch")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "dispatch resource not found")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid dispatch input")
	case errors.Is(err, ErrInvalidStatus):
		response.Error(w, http.StatusUnprocessableEntity, "invalid_status_transition", "this status transition is not allowed")
	case errors.Is(err, ErrDuplicate):
		response.Error(w, http.StatusConflict, "duplicate_dispatch", "dispatch already exists with this dispatch number")
	case errors.Is(err, ErrAlreadyAccepted):
		response.Error(w, http.StatusConflict, "dispatch_already_accepted", "this trip has already been accepted by a driver")
	default:
		response.Error(w, http.StatusInternalServerError, "dispatches_error", "dispatch request failed")
	}
}
