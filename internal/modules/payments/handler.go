package payments

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
//	@Summary	List payments
//	@Tags		Payments
//	@Security	BearerAuth
//	@Success	200	{object}	PaymentsResponse
//	@Router		/api/v1/payments [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	payments, pagination, err := h.service.List(r.Context(), actor, listRequest(r))
	if err != nil {
		writePaymentsError(w, err)
		return
	}
	response.OK(w, PaymentsResponse{Payments: payments, Pagination: pagination})
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
	req.PaymentFor = paymentForOrder
	payments, pagination, err := h.service.List(r.Context(), actor, req)
	if err != nil {
		writePaymentsError(w, err)
		return
	}
	response.OK(w, PaymentsResponse{Payments: payments, Pagination: pagination})
}

func (h *Handler) ListBySubscription(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	subscriptionID, ok := pathID(w, r, "subscriptionId")
	if !ok {
		return
	}
	req := listRequest(r)
	req.SubscriptionID = subscriptionID
	req.PaymentFor = paymentForSubscription
	payments, pagination, err := h.service.List(r.Context(), actor, req)
	if err != nil {
		writePaymentsError(w, err)
		return
	}
	response.OK(w, PaymentsResponse{Payments: payments, Pagination: pagination})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	paymentID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	payment, err := h.service.Get(r.Context(), actor, paymentID)
	if err != nil {
		writePaymentsError(w, err)
		return
	}
	response.OK(w, PaymentResponse{Payment: payment})
}

func (h *Handler) CreateManual(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req ManualPaymentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	payment, err := h.service.CreateManual(r.Context(), actor, req)
	if err != nil {
		writePaymentsError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, PaymentResponse{Payment: payment})
}

func (h *Handler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	paymentID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req UpdateStatusRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	payment, err := h.service.UpdateStatus(r.Context(), actor, paymentID, req)
	if err != nil {
		writePaymentsError(w, err)
		return
	}
	response.OK(w, PaymentResponse{Payment: payment})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return actor.AsActorContext(), true
}

func listRequest(r *http.Request) ListPaymentsRequest {
	query := r.URL.Query()
	return ListPaymentsRequest{
		Page:           intQuery(query.Get("page")),
		PerPage:        intQuery(query.Get("per_page")),
		Search:         query.Get("search"),
		PaymentFor:     query.Get("payment_for"),
		OrderID:        int64Query(query.Get("order_id")),
		SubscriptionID: int64Query(query.Get("subscription_id")),
		PayerUserID:    int64Query(query.Get("payer_user_id")),
		Status:         query.Get("payment_status"),
		Method:         query.Get("payment_method"),
		SortBy:         query.Get("sort_by"),
		SortOrder:      query.Get("sort_order"),
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

func writePaymentsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to access payment")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "payment resource not found")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid payment input")
	default:
		response.Error(w, http.StatusInternalServerError, "payments_error", "payment request failed")
	}
}
