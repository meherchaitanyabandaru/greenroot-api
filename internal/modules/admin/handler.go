package admin

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
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	s, err := h.service.Dashboard(r.Context(), a)
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			response.Error(w, 403, "forbidden", "admin only")
		} else {
			response.Error(w, 500, "admin_error", "admin request failed")
		}
		return
	}
	response.OK(w, DashboardResponse{Summary: s})
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	users, pagination, err := h.service.ListUsers(r.Context(), a, listUsersRequest(r))
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			response.Error(w, http.StatusForbidden, "forbidden", "admin only")
		} else {
			response.Error(w, http.StatusInternalServerError, "admin_users_error", "admin users request failed")
		}
		return
	}
	response.OK(w, UsersResponse{Users: users, Pagination: pagination})
}

func (h *Handler) UpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	idStr := chi.URLParam(r, "id")
	userID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || userID <= 0 {
		response.Error(w, http.StatusBadRequest, "invalid_id", "invalid user id")
		return
	}
	var req UpdateUserStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	if err := h.service.UpdateUserStatus(r.Context(), a, userID, req); err != nil {
		switch {
		case errors.Is(err, ErrForbidden):
			response.Error(w, http.StatusForbidden, "forbidden", "admin only")
		default:
			response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
		}
		return
	}
	response.OK(w, map[string]string{"message": "user status updated"})
}

func (h *Handler) UpdateNurseryStatus(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	idStr := chi.URLParam(r, "id")
	nurseryID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || nurseryID <= 0 {
		response.Error(w, http.StatusBadRequest, "invalid_id", "invalid nursery id")
		return
	}
	var req UpdateNurseryStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	if err := h.service.UpdateNurseryStatus(r.Context(), a, nurseryID, req); err != nil {
		switch {
		case errors.Is(err, ErrForbidden):
			response.Error(w, http.StatusForbidden, "forbidden", "admin only")
		default:
			response.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
		}
		return
	}
	response.OK(w, map[string]string{"message": "nursery status updated"})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return ActorContext{UserID: actor.UserID, Roles: actor.Roles, IPAddress: actor.IPAddress, UserAgent: actor.UserAgent}, true
}

func listUsersRequest(r *http.Request) ListUsersRequest {
	query := r.URL.Query()
	return ListUsersRequest{
		Page:    intQuery(query.Get("page")),
		PerPage: intQuery(query.Get("per_page")),
		Search:  query.Get("search"),
		Status:  query.Get("status"),
		Role:    query.Get("role"),
	}
}

func intQuery(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}
