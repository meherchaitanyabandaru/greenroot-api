package subscriptions

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

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return ActorContext{UserID: actor.UserID, Roles: actor.Roles, IPAddress: actor.IPAddress, UserAgent: actor.UserAgent}, true
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
	case errors.Is(err, ErrConflict):
		response.Error(w, http.StatusConflict, "active_subscription_exists", "user already has an active subscription")
	default:
		response.Error(w, http.StatusInternalServerError, "subscriptions_error", "subscription request failed")
	}
}
