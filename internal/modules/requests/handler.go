package requests

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
//	@Summary	List plant requests
//	@Tags		Plant Requests
//	@Security	BearerAuth
//	@Success	200	{object}	RequestsResponse
//	@Router		/api/v1/plant-requests [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	requests, pagination, err := h.service.List(r.Context(), actor, listRequest(r))
	if err != nil {
		writeRequestsError(w, err)
		return
	}
	response.OK(w, RequestsResponse{Requests: requests, Pagination: pagination})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	requestID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	request, err := h.service.Get(r.Context(), actor, requestID)
	if err != nil {
		writeRequestsError(w, err)
		return
	}
	response.OK(w, RequestResponse{Request: request})
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
	request, err := h.service.Create(r.Context(), actor, req)
	if err != nil {
		writeRequestsError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, RequestResponse{Request: request})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	requestID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req UpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	request, err := h.service.Update(r.Context(), actor, requestID, req)
	if err != nil {
		writeRequestsError(w, err)
		return
	}
	response.OK(w, RequestResponse{Request: request})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	requestID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), actor, requestID); err != nil {
		writeRequestsError(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "Plant request cancelled successfully"})
}

func (h *Handler) ListResponses(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	requestID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	responses, err := h.service.ListResponses(r.Context(), actor, requestID)
	if err != nil {
		writeRequestsError(w, err)
		return
	}
	response.OK(w, ResponsesResponse{Responses: responses})
}

func (h *Handler) CreateResponse(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	requestID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req CreateResponseRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	plantResponse, err := h.service.CreateResponse(r.Context(), actor, requestID, req)
	if err != nil {
		writeRequestsError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, SingleResponse{Response: plantResponse})
}

func (h *Handler) UpdateResponse(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	responseID, ok := pathID(w, r, "responseId")
	if !ok {
		return
	}
	var req UpdateResponseRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	plantResponse, err := h.service.UpdateResponse(r.Context(), actor, responseID, req)
	if err != nil {
		writeRequestsError(w, err)
		return
	}
	response.OK(w, SingleResponse{Response: plantResponse})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return ActorContext{UserID: actor.UserID, Roles: actor.Roles, IPAddress: actor.IPAddress, UserAgent: actor.UserAgent}, true
}

func listRequest(r *http.Request) ListRequestsRequest {
	query := r.URL.Query()
	return ListRequestsRequest{
		Page:      intQuery(query.Get("page")),
		PerPage:   intQuery(query.Get("per_page")),
		NurseryID: int64Query(query.Get("nursery_id")),
		PlantID:   int64Query(query.Get("plant_id")),
		Status:    query.Get("status"),
		Search:    query.Get("search"),
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

func writeRequestsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to manage plant requests")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "plant request resource not found")
	case errors.Is(err, ErrInsufficientInventory):
		response.Error(w, http.StatusConflict, "insufficient_inventory", "supplier inventory cannot satisfy this response")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid plant request input")
	default:
		response.Error(w, http.StatusInternalServerError, "plant_requests_error", "plant request failed")
	}
}
