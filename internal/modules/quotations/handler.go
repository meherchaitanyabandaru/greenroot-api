package quotations

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

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
	req := listRequest(r)
	// Scoping is fully handled by service.scopeList
	qs, pagination, err := h.service.List(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, QuotationsResponse{Quotations: qs, Pagination: pagination})
}

func (h *Handler) BuyerAccept(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	q, err := h.service.BuyerAccept(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, QuotationResponse{Quotation: q})
}

func (h *Handler) BuyerReject(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req AcceptRejectQuotationRequest
	_ = decodeJSON(w, r, &req) // reason is optional; ignore decode error
	q, err := h.service.BuyerReject(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, QuotationResponse{Quotation: q})
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
	q, err := h.service.Get(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, QuotationResponse{Quotation: q})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	rawActor, _ := authctx.ActorFromContext(r.Context())
	if !authctx.RequireActiveNursery(w, rawActor) || !authctx.RequireActiveSubscription(w, rawActor) {
		return
	}
	var req CreateQuotationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	q, err := h.service.Create(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, QuotationResponse{Quotation: q})
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
	var req UpdateQuotationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	q, err := h.service.Update(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, QuotationResponse{Quotation: q})
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
	response.OK(w, MessageResponse{Message: "Quotation deleted successfully"})
}

func (h *Handler) AssignManager(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req AssignManagerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	q, err := h.service.AssignManager(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, QuotationResponse{Quotation: q})
}

func (h *Handler) UnassignManager(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	q, err := h.service.UnassignManager(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, QuotationResponse{Quotation: q})
}

func (h *Handler) Approve(w http.ResponseWriter, r *http.Request) {
	h.SendToCustomer(w, r)
}

func (h *Handler) SendToCustomer(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	q, err := h.service.SendToCustomer(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, QuotationResponse{Quotation: q})
}

func (h *Handler) Recall(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	q, err := h.service.Recall(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, QuotationResponse{Quotation: q})
}

func (h *Handler) ConvertToOrder(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	q, err := h.service.ConvertToOrder(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, QuotationResponse{Quotation: q})
}

func (h *Handler) RecordDownload(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Masked bool `json:"masked"`
	}
	// Body is optional — ignore decode errors (empty body is valid; masked defaults to false).
	_ = json.NewDecoder(r.Body).Decode(&req)
	r.Body.Close()
	if err := h.service.RecordDownload(r.Context(), actor, id, req.Masked); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return ActorContext{UserID: actor.UserID, Roles: actor.Roles, IPAddress: actor.IPAddress, UserAgent: actor.UserAgent}, true
}

func listRequest(r *http.Request) ListQuotationsRequest {
	q := r.URL.Query()
	return ListQuotationsRequest{
		Page:           intQuery(q.Get("page")),
		PerPage:        intQuery(q.Get("per_page")),
		Search:         q.Get("search"),
		NurseryID:      int64Query(q.Get("nursery_id")),
		Status:         q.Get("status"),
		SortBy:         q.Get("sort_by"),
		SortOrder:      q.Get("sort_order"),
		Buying:         q.Get("buying") == "true" || q.Get("buying") == "1",
		UnassignedOnly: q.Get("unassigned") == "true" || q.Get("unassigned") == "1",
		DateFrom:       timeQuery(q.Get("date_from")),
		DateTo:         timeQuery(q.Get("date_to")),
		AmountMin:      float64PtrQuery(q.Get("amount_min")),
		AmountMax:      float64PtrQuery(q.Get("amount_max")),
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

func intQuery(v string) int {
	n, _ := strconv.Atoi(v)
	return n
}

func int64Query(v string) int64 {
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

func timeQuery(v string) *time.Time {
	if v == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return nil
	}
	return &t
}

func float64PtrQuery(v string) *float64 {
	if v == "" {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	return &f
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to access quotation")
	case errors.Is(err, ErrAlreadyConverted):
		response.Error(w, http.StatusConflict, "already_converted", "quotation has already been converted to an order")
	case errors.Is(err, ErrInvalidTransition):
		response.Error(w, http.StatusConflict, "invalid_transition", "action not allowed in current quotation status")
	case errors.Is(err, ErrQuotationExpired):
		response.Error(w, http.StatusConflict, "quotation_expired", "quotation has expired and can no longer be accepted")
	case errors.Is(err, ErrCustomerRequired):
		response.Error(w, http.StatusBadRequest, "customer_required", "customer information required for customer quotations")
	case errors.Is(err, ErrPlantNotFound):
		response.Error(w, http.StatusNotFound, "plant_not_found", err.Error())
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "quotation not found")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid quotation input")
	default:
		response.Error(w, http.StatusInternalServerError, "quotation_error", "quotation request failed")
	}
}
