package search

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Handler struct {
	service *Service
	jwt     *jwtplatform.Service
}

func NewHandler(s *Service, j *jwtplatform.Service) *Handler {
	return &Handler{service: s, jwt: j}
}

type recentSearchRequest struct {
	Query string `json:"query"`
}

type recentSearchesResponse struct {
	RecentSearches []string `json:"recent_searches"`
}

type suggestionsResponse struct {
	Suggestions []string `json:"suggestions"`
}

// GetRecent returns the caller's recent searches, most recent first, max 10.
func (h *Handler) GetRecent(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	response.OK(w, recentSearchesResponse{RecentSearches: h.service.RecentSearches(r.Context(), actor.UserID)})
}

// RecordRecent records one query in the caller's recent-search history.
// The mobile client calls this once per committed search (not per
// keystroke during the 300ms debounce).
func (h *Handler) RecordRecent(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req recentSearchRequest
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return
	}
	h.service.RecordSearch(r.Context(), actor.UserID, req.Query)
	response.OK(w, map[string]bool{"recorded": true})
}

// ClearRecent wipes the caller's recent-search history. Call on logout so
// a shared/reset device doesn't carry over the previous user's history.
func (h *Handler) ClearRecent(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	h.service.ClearRecentSearches(r.Context(), actor.UserID)
	response.OK(w, map[string]bool{"cleared": true})
}

// Suggestions returns the top popular search terms for ?module= (plants,
// orders, quotations, market_ads -- defaults to plants), derived from real
// search traffic, not a hardcoded list.
func (h *Handler) Suggestions(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.actor(w, r); !ok {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.ParseInt(q.Get("limit"), 10, 64)
	response.OK(w, suggestionsResponse{Suggestions: h.service.Suggestions(r.Context(), q.Get("module"), limit)})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (authctx.ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return authctx.ActorContext{}, false
	}
	return actor.AsActorContext(), true
}
