package admin

import (
	"errors"
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
