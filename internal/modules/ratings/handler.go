package ratings

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

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return ActorContext{UserID: actor.UserID, Roles: actor.Roles}, true
}

// SubmitApp godoc: POST /ratings/app
func (h *Handler) SubmitApp(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req SubmitAppRatingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	rating, err := h.service.SubmitApp(r.Context(), actor, req)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response.OK(w, RatingResponse{Rating: *rating})
}

// SubmitTrip godoc: POST /ratings/trip/:dispatch_id
func (h *Handler) SubmitTrip(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	dispatchID, err := strconv.ParseInt(chi.URLParam(r, "dispatch_id"), 10, 64)
	if err != nil || dispatchID <= 0 {
		response.Error(w, http.StatusBadRequest, "invalid_id", "invalid dispatch_id")
		return
	}
	var req SubmitTripRatingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	rating, err := h.service.SubmitTrip(r.Context(), actor, dispatchID, req)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response.OK(w, RatingResponse{Rating: *rating})
}

// SubmitOrder godoc: POST /ratings/order/:order_id
func (h *Handler) SubmitOrder(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orderID, err := strconv.ParseInt(chi.URLParam(r, "order_id"), 10, 64)
	if err != nil || orderID <= 0 {
		response.Error(w, http.StatusBadRequest, "invalid_id", "invalid order_id")
		return
	}
	var req SubmitOrderRatingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	rating, err := h.service.SubmitOrder(r.Context(), actor, orderID, req)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response.OK(w, RatingResponse{Rating: *rating})
}

// GetMyOrderRating godoc: GET /ratings/order/:order_id
func (h *Handler) GetMyOrderRating(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	orderID, err := strconv.ParseInt(chi.URLParam(r, "order_id"), 10, 64)
	if err != nil || orderID <= 0 {
		response.Error(w, http.StatusBadRequest, "invalid_id", "invalid order_id")
		return
	}
	rating, err := h.service.GetMyOrderRating(r.Context(), actor, orderID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"rating": rating})
}

// GetMyTripRating godoc: GET /ratings/trip/:dispatch_id
func (h *Handler) GetMyTripRating(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	dispatchID, err := strconv.ParseInt(chi.URLParam(r, "dispatch_id"), 10, 64)
	if err != nil || dispatchID <= 0 {
		response.Error(w, http.StatusBadRequest, "invalid_id", "invalid dispatch_id")
		return
	}
	rating, err := h.service.GetMyTripRating(r.Context(), actor, dispatchID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"rating": rating})
}

// List godoc: GET /ratings  (admin only)
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	ratings, err := h.service.List(r.Context(), actor, ListRatingsRequest{
		RatingType: r.URL.Query().Get("type"),
		Page:       page,
		PerPage:    perPage,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	if ratings == nil {
		ratings = []Rating{}
	}
	response.OK(w, RatingsResponse{Ratings: ratings})
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid rating input")
	default:
		response.Error(w, http.StatusInternalServerError, "ratings_error", "ratings request failed")
	}
}
