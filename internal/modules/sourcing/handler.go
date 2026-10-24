package sourcing

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

// ---- Network membership ----

// GetMembership godoc
//
//	@Summary	Get nursery sourcing network membership
//	@Tags		Sourcing Network
//	@Security	BearerAuth
//	@Param		nurseryId	path	int	true	"Nursery ID"
//	@Success	200	{object}	MemberResponse
//	@Router		/api/v1/nurseries/{nurseryId}/sourcing-membership [get]
func (h *Handler) GetMembership(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "nurseryId")
	if !ok {
		return
	}
	m, err := h.service.GetMembership(r.Context(), actor, nurseryID)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, MemberResponse{Member: m})
}

// JoinNetwork godoc
//
//	@Summary	Join the Plant Sourcing Network
//	@Tags		Sourcing Network
//	@Security	BearerAuth
//	@Param		nurseryId	path	int	true	"Nursery ID"
//	@Success	200	{object}	MemberResponse
//	@Router		/api/v1/nurseries/{nurseryId}/sourcing-membership [post]
func (h *Handler) JoinNetwork(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "nurseryId")
	if !ok {
		return
	}
	var req JoinNetworkRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	m, err := h.service.JoinNetwork(r.Context(), actor, nurseryID, req)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, MemberResponse{Member: m})
}

// LeaveNetwork godoc
//
//	@Summary	Leave the Plant Sourcing Network
//	@Tags		Sourcing Network
//	@Security	BearerAuth
//	@Param		nurseryId	path	int	true	"Nursery ID"
//	@Router		/api/v1/nurseries/{nurseryId}/sourcing-membership [delete]
func (h *Handler) LeaveNetwork(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "nurseryId")
	if !ok {
		return
	}
	if err := h.service.LeaveNetwork(r.Context(), actor, nurseryID); err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, map[string]string{"message": "left sourcing network"})
}

// ---- Discovery ----

// ListNearby godoc
//
//	@Summary	Discover nearby nurseries in the Plant Sourcing Network
//	@Tags		Sourcing Network
//	@Security	BearerAuth
//	@Success	200	{object}	NearbyNurseriesResponse
//	@Router		/api/v1/sourcing-network/nurseries [get]
func (h *Handler) ListNearby(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	q := nearbyQuery(r)
	nurseries, pg, err := h.service.ListNearby(r.Context(), actor, q)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, NearbyNurseriesResponse{Nurseries: nurseries, Pagination: pg})
}

// GetNurseryProfile godoc
//
//	@Summary	Get a network nursery's sourcing profile
//	@Tags		Sourcing Network
//	@Security	BearerAuth
//	@Param		nurseryId	path	int	true	"Nursery ID"
//	@Success	200	{object}	NearbyNursery
//	@Router		/api/v1/sourcing-network/nurseries/{nurseryId} [get]
func (h *Handler) GetNurseryProfile(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, ok := pathID(w, r, "nurseryId")
	if !ok {
		return
	}
	nn, err := h.service.GetNurseryProfile(r.Context(), actor, nurseryID)
	if err != nil {
		writeSourcingError(w, err)
		return
	}
	response.OK(w, nn)
}

// ---- Sourcing posts ----

// ---- Post responses ----

// ---- Internal helpers ----

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

func nearbyQuery(r *http.Request) NearbyQuery {
	q := NearbyQuery{
		PlantName: r.URL.Query().Get("plant_name"),
		Page:      intParam(r, "page", 1),
		PerPage:   intParam(r, "per_page", 20),
		RadiusKM:  intParam(r, "radius_km", 50),
	}
	if lat := r.URL.Query().Get("latitude"); lat != "" {
		if v, err := strconv.ParseFloat(lat, 64); err == nil {
			q.Latitude = &v
		}
	}
	if lon := r.URL.Query().Get("longitude"); lon != "" {
		if v, err := strconv.ParseFloat(lon, 64); err == nil {
			q.Longitude = &v
		}
	}
	return q
}

func intParam(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func writeSourcingError(w http.ResponseWriter, err error) {
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
