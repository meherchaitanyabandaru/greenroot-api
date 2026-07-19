package tracking

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

func NewHandler(s *Service, j *jwtplatform.Service) *Handler { return &Handler{service: s, jwt: j} }
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CreateRequest
	if !decode(w, r, &req) {
		return
	}
	p, err := h.service.Create(r.Context(), a, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	response.JSON(w, 201, PointResponse{Tracking: &p})
}
func (h *Handler) UpdateLiveLocation(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req LiveLocationRequest
	if !decode(w, r, &req) {
		return
	}
	loc, err := h.service.UpdateLiveLocation(r.Context(), a, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	response.OK(w, LiveLocationResponse{Location: loc})
}
func (h *Handler) GetLiveDriver(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "driverUserId")
	if !ok {
		return
	}
	loc, err := h.service.GetLiveDriver(r.Context(), a, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	response.OK(w, LiveLocationResponse{Location: loc})
}
func (h *Handler) NearbyLiveDrivers(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	latitude, ok := queryFloat(w, r, "latitude")
	if !ok {
		return
	}
	longitude, ok := queryFloat(w, r, "longitude")
	if !ok {
		return
	}
	radiusKM, ok := queryFloat(w, r, "radius_km")
	if !ok {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			response.Error(w, 400, "invalid_limit", "invalid limit")
			return
		}
		limit = parsed
	}
	drivers, err := h.service.NearbyLiveDrivers(r.Context(), a, latitude, longitude, radiusKM, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	response.OK(w, NearbyLiveDriversResponse{Drivers: drivers})
}
func (h *Handler) ListDispatch(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, "dispatch_id", "dispatchId")
}
func (h *Handler) LatestDispatch(w http.ResponseWriter, r *http.Request) {
	h.latest(w, r, "dispatch_id", "dispatchId")
}
func (h *Handler) ListDriver(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, "driver_id", "driverId")
}
func (h *Handler) LatestDriver(w http.ResponseWriter, r *http.Request) {
	h.latest(w, r, "driver_id", "driverId")
}
func (h *Handler) ListVehicle(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, "vehicle_id", "vehicleId")
}
func (h *Handler) LatestVehicle(w http.ResponseWriter, r *http.Request) {
	h.latest(w, r, "vehicle_id", "vehicleId")
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request, col, key string) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, key)
	if !ok {
		return
	}
	rows, err := h.service.List(r.Context(), a, col, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	response.OK(w, ListResponse{Tracking: rows})
}
func (h *Handler) latest(w http.ResponseWriter, r *http.Request, col, key string) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, key)
	if !ok {
		return
	}
	p, err := h.service.Latest(r.Context(), a, col, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	response.OK(w, PointResponse{Tracking: p})
}
func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return actor.AsActorContext(), true
}
func decode(w http.ResponseWriter, r *http.Request, d any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(d); err != nil {
		response.Error(w, 400, "invalid_json", "invalid JSON request body")
		return false
	}
	return true
}
func pathID(w http.ResponseWriter, r *http.Request, k string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, k), 10, 64)
	if err != nil || id <= 0 {
		response.Error(w, 400, "invalid_id", "invalid id")
		return 0, false
	}
	return id, true
}
func queryFloat(w http.ResponseWriter, r *http.Request, k string) (float64, bool) {
	value, err := strconv.ParseFloat(r.URL.Query().Get(k), 64)
	if err != nil {
		response.Error(w, 400, "invalid_"+k, "invalid "+k)
		return 0, false
	}
	return value, true
}
func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, 403, "forbidden", "not allowed")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, 400, "invalid_input", "invalid tracking input")
	default:
		response.Error(w, 500, "tracking_error", "tracking request failed")
	}
}
