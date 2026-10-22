package subscriptions

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	"github.com/meherchaitanyabandaru/greenroot-api/platform/razorpay"
)

type Handler struct {
	service *Service
	jwt     *jwtplatform.Service
}

func NewHandler(service *Service, jwt *jwtplatform.Service) *Handler {
	return &Handler{service: service, jwt: jwt}
}

// ListPlans godoc
//
//	@Summary	List subscription plans
//	@Tags		Subscriptions
//	@Success	200	{object}	PlansResponse
//	@Router		/api/v1/subscription-plans [get]
func (h *Handler) ListPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := h.service.ListPlans(r.Context())
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, PlansResponse{Plans: plans})
}

func (h *Handler) GetPlan(w http.ResponseWriter, r *http.Request) {
	planID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	plan, err := h.service.GetPlan(r.Context(), planID)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, PlanResponse{Plan: plan})
}

func (h *Handler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	planID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req UpdatePlanRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	plan, err := h.service.UpdatePlan(r.Context(), actor, planID, req)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, PlanResponse{Plan: plan})
}

// List godoc
//
//	@Summary	List subscriptions
//	@Tags		Subscriptions
//	@Security	BearerAuth
//	@Success	200	{object}	SubscriptionsResponse
//	@Router		/api/v1/subscriptions [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	subscriptions, pagination, err := h.service.List(r.Context(), actor, listRequest(r))
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, SubscriptionsResponse{Subscriptions: subscriptions, Pagination: pagination})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	subscriptions, pagination, err := h.service.Me(r.Context(), actor)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, SubscriptionsResponse{Subscriptions: subscriptions, Pagination: pagination})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	subscriptionID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	subscription, err := h.service.Get(r.Context(), actor, subscriptionID)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, SubscriptionResponse{Subscription: subscription})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CreateSubscriptionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	subscription, err := h.service.Create(r.Context(), actor, req)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, SubscriptionResponse{Subscription: subscription})
}

// CreatePaymentOrder starts a Razorpay checkout for a new subscription or a renewal.
func (h *Handler) CreatePaymentOrder(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req PaymentOrderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	order, err := h.service.CreatePaymentOrder(r.Context(), actor, req)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, PaymentOrderResponse{PaymentOrder: order})
}

func (h *Handler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	subscriptionID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req UpdateStatusRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	subscription, err := h.service.UpdateStatus(r.Context(), actor, subscriptionID, req)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, SubscriptionResponse{Subscription: subscription})
}

func (h *Handler) Renew(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	subscriptionID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req RenewSubscriptionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	subscription, err := h.service.Renew(r.Context(), actor, subscriptionID, req)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, SubscriptionResponse{Subscription: subscription})
}

func (h *Handler) ListPayments(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	subscriptionID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	payments, err := h.service.ListPayments(r.Context(), actor, subscriptionID)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, PaymentsResponse{Payments: payments})
}

// RefundPayment: admin refunds a subscription payment in full (see refund.go).
func (h *Handler) RefundPayment(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	subscriptionID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	paymentID, ok := pathID(w, r, "paymentID")
	if !ok {
		return
	}
	var req RefundRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	subscription, err := h.service.RefundPayment(r.Context(), actor, subscriptionID, paymentID, req)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, SubscriptionResponse{Subscription: subscription})
}

func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	subscriptionID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req CancelSubscriptionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	subscription, err := h.service.Cancel(r.Context(), actor, subscriptionID, req)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, SubscriptionResponse{Subscription: subscription})
}

// ── Promo handlers ────────────────────────────────────────────────────────────

func (h *Handler) ListPromos(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	promos, err := h.service.ListPromos(r.Context(), actor)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, PromosResponse{Promos: promos})
}

func (h *Handler) CreatePromo(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CreatePromoRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	promo, err := h.service.CreatePromo(r.Context(), actor, req)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, PromoResponse{Promo: promo})
}

func (h *Handler) UpdatePromo(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	promoID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req UpdatePromoRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	promo, err := h.service.UpdatePromo(r.Context(), actor, promoID, req)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, PromoResponse{Promo: promo})
}

func (h *Handler) ValidatePromo(w http.ResponseWriter, r *http.Request) {
	_, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req ValidatePromoRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result := h.service.ValidatePromo(r.Context(), req)
	response.OK(w, PromoValidationResponse{Validation: result})
}

func (h *Handler) BlastPromo(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	promoID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	count, err := h.service.BlastPromo(r.Context(), actor, promoID)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, BlastResponse{SentCount: count})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return actor.AsActorContext(), true
}

func listRequest(r *http.Request) ListSubscriptionsRequest {
	query := r.URL.Query()
	return ListSubscriptionsRequest{
		Page:    intQuery(query.Get("page")),
		PerPage: intQuery(query.Get("per_page")),
		UserID:  int64Query(query.Get("user_id")),
		Status:  query.Get("subscription_status"),
		Search:  query.Get("search"),
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

func writeSubscriptionsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to access subscription")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "subscription resource not found")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid subscription input")
	case errors.Is(err, ErrPaused):
		response.Error(w, http.StatusConflict, "subscription_paused", "this subscription is paused by GreenRoot — contact support")
	case errors.Is(err, ErrConflict):
		response.Error(w, http.StatusConflict, "active_subscription_exists", "user already has an active subscription")
	case errors.Is(err, redisutil.ErrLockBusy):
		response.Error(w, http.StatusConflict, "resource_locked", "another update is already in progress")
	case errors.Is(err, ErrGatewayDisabled):
		response.Error(w, http.StatusServiceUnavailable, "payments_unavailable", "online payments are not configured")
	case errors.Is(err, ErrPaymentRequired):
		// 400, not 402: apps read 402 as "nursery subscription expired".
		response.Error(w, http.StatusBadRequest, "payment_required", "pay online to continue — update the app if you don't see the payment screen")
	case errors.Is(err, ErrPaymentInvalid):
		response.Error(w, http.StatusBadRequest, "payment_verification_failed", "payment could not be verified")
	case errors.Is(err, ErrPaymentUsed):
		response.Error(w, http.StatusConflict, "payment_already_used", "this payment was already used")
	case errors.As(err, new(RefundError)):
		var re RefundError
		errors.As(err, &re)
		response.Error(w, http.StatusConflict, "refund_not_allowed", re.Reason)
	case errors.Is(err, razorpay.ErrRejected):
		response.Error(w, http.StatusConflict, "payment_gateway_rejected", strings.TrimPrefix(err.Error(), razorpay.ErrRejected.Error()+": "))
	case errors.Is(err, razorpay.ErrAuth), errors.Is(err, razorpay.ErrGateway):
		// 502, not 401: a 401 would make the app sign the user out.
		slog.Error("razorpay request failed", "error", err)
		response.Error(w, http.StatusBadGateway, "payment_gateway_error", "payment service is unavailable, try again")
	default:
		response.Error(w, http.StatusInternalServerError, "subscriptions_error", "subscription request failed")
	}
}
