package market

import (
	"context"
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
	svc *Service
	jwt *jwtplatform.Service
}

func NewHandler(svc *Service, jwt *jwtplatform.Service) *Handler {
	return &Handler{svc: svc, jwt: jwt}
}

// ── Ads ───────────────────────────────────────────────────────

func (h *Handler) BrowseAds(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	minPrice, _ := strconv.ParseFloat(r.URL.Query().Get("min_price"), 64)
	maxPrice, _ := strconv.ParseFloat(r.URL.Query().Get("max_price"), 64)
	q := AdsQuery{
		Search:   r.URL.Query().Get("q"),
		Sort:     r.URL.Query().Get("sort"),
		Category: r.URL.Query().Get("category"),
		MinPrice: minPrice,
		MaxPrice: maxPrice,
		NearLat:  floatQueryParam(r, "near_lat"),
		NearLon:  floatQueryParam(r, "near_lon"),
		RadiusKM: floatQueryParam(r, "radius_km"),
		Page:     intParam(r, "page", 1),
		PerPage:  intParam(r, "per_page", 20),
	}
	ads, total, err := h.svc.BrowseAds(r.Context(), actor, q)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, AdsResponse{Ads: ads, Total: total, Page: q.Page, PerPage: q.PerPage})
}

func (h *Handler) CreateAd(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	rawActor, _ := authctx.ActorFromContext(r.Context())
	if !authctx.RequireActiveNursery(w, rawActor) || !authctx.RequireActiveSubscription(w, rawActor) {
		return
	}
	var req CreateAdRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ad, err := h.svc.CreateAd(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, AdResponse{Ad: ad})
}

func (h *Handler) MyAds(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	q := AdsQuery{
		Page:    intParam(r, "page", 1),
		PerPage: intParam(r, "per_page", 20),
	}
	ads, total, err := h.svc.MyAds(r.Context(), actor, q)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, AdsResponse{Ads: ads, Total: total, Page: q.Page, PerPage: q.PerPage})
}

func (h *Handler) GetAd(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	ad, err := h.svc.GetAd(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, AdResponse{Ad: ad})
}

func (h *Handler) UpdateAd(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req UpdateAdRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ad, err := h.svc.UpdateAd(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, AdResponse{Ad: ad})
}

func (h *Handler) PublishAd(w http.ResponseWriter, r *http.Request) {
	h.adTransition(w, r, h.svc.PublishAd)
}

func (h *Handler) PauseAd(w http.ResponseWriter, r *http.Request) {
	h.adTransition(w, r, h.svc.PauseAd)
}

func (h *Handler) ResumeAd(w http.ResponseWriter, r *http.Request) {
	h.adTransition(w, r, h.svc.ResumeAd)
}

func (h *Handler) RenewAd(w http.ResponseWriter, r *http.Request) {
	h.adTransition(w, r, h.svc.RenewAd)
}

func (h *Handler) ArchiveAd(w http.ResponseWriter, r *http.Request) {
	h.adTransition(w, r, h.svc.ArchiveAd)
}

func (h *Handler) adTransition(
	w http.ResponseWriter,
	r *http.Request,
	fn func(context.Context, ActorContext, int64) (Ad, error),
) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	ad, err := fn(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, AdResponse{Ad: ad})
}

func (h *Handler) ToggleSave(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	saved, err := h.svc.ToggleSave(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, SaveToggleResponse{Saved: saved})
}

func (h *Handler) SavedAds(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	q := AdsQuery{
		Page:    intParam(r, "page", 1),
		PerPage: intParam(r, "per_page", 20),
	}
	ads, total, err := h.svc.SavedAds(r.Context(), actor, q)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, AdsResponse{Ads: ads, Total: total, Page: q.Page, PerPage: q.PerPage})
}

func (h *Handler) ReportAd(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req ReportAdRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.svc.ReportAd(r.Context(), actor, id, req); err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]string{"message": "report submitted"})
}

// ── Enquiries ─────────────────────────────────────────────────

func (h *Handler) SendEnquiry(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req CreateEnquiryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	enquiry, err := h.svc.SendEnquiry(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, EnquiryResponse{Enquiry: enquiry})
}

func (h *Handler) ListEnquiries(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	q := EnquiriesQuery{
		Direction: r.URL.Query().Get("direction"),
		Status:    r.URL.Query().Get("status"),
		Page:      intParam(r, "page", 1),
		PerPage:   intParam(r, "per_page", 20),
	}
	enquiries, total, err := h.svc.ListEnquiries(r.Context(), actor, q)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, EnquiriesResponse{Enquiries: enquiries, Total: total, Page: q.Page, PerPage: q.PerPage})
}

func (h *Handler) GetEnquiry(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	enquiry, err := h.svc.GetEnquiry(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, EnquiryResponse{Enquiry: enquiry})
}

func (h *Handler) ReplyToEnquiry(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req ReplyEnquiryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	msg, err := h.svc.ReplyToEnquiry(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, map[string]any{"message": msg})
}

func (h *Handler) CloseEnquiry(w http.ResponseWriter, r *http.Request) {
	h.enquiryTransition(w, r, h.svc.CloseEnquiry)
}

func (h *Handler) CancelEnquiry(w http.ResponseWriter, r *http.Request) {
	h.enquiryTransition(w, r, h.svc.CancelEnquiry)
}

func (h *Handler) enquiryTransition(
	w http.ResponseWriter,
	r *http.Request,
	fn func(context.Context, ActorContext, int64) (Enquiry, error),
) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	e, err := fn(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, EnquiryResponse{Enquiry: e})
}

func (h *Handler) LinkQuotation(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req LinkQuotationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	e, err := h.svc.LinkQuotation(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, EnquiryResponse{Enquiry: e})
}

// ── Helpers ───────────────────────────────────────────────────

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	c, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return ActorContext{
		UserID:    c.UserID,
		Roles:     c.Roles,
		IPAddress: c.IPAddress,
		UserAgent: c.UserAgent,
	}, true
}

func pathID(w http.ResponseWriter, r *http.Request, param string) (int64, bool) {
	v := chi.URLParam(r, param)
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		response.Error(w, http.StatusBadRequest, "invalid_param", param+" must be a positive integer")
		return 0, false
	}
	return id, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return false
	}
	return true
}

func floatQueryParam(r *http.Request, key string) *float64 {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	return &f
}

func intParam(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
	default:
		response.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
	}
}
