package audit

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
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	rows, p, err := h.service.List(r.Context(), a, listReq(r))
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			response.Error(w, 403, "forbidden", "admin only")
		} else {
			response.Error(w, 500, "audit_error", "audit request failed")
		}
		return
	}
	response.OK(w, ListResponse{AuditLogs: rows, Pagination: p})
}
func (h *Handler) ListSecurity(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	in := ListSecurityRequest{
		Page:      intQ(q.Get("page")),
		PerPage:   intQ(q.Get("per_page")),
		EventType: q.Get("event_type"),
		UserID:    int64Q(q.Get("user_id")),
	}
	rows, p, err := h.service.ListSecurity(r.Context(), a, in)
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			response.Error(w, 403, "forbidden", "admin only")
		} else {
			response.Error(w, 500, "security_log_error", "security log request failed")
		}
		return
	}
	response.OK(w, ListSecurityResponse{SecurityLogs: rows, Pagination: p})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return ActorContext{UserID: actor.UserID, Roles: actor.Roles, IPAddress: actor.IPAddress, UserAgent: actor.UserAgent}, true
}
func listReq(r *http.Request) ListRequest {
	q := r.URL.Query()
	return ListRequest{
		Page:       intQ(q.Get("page")),
		PerPage:    intQ(q.Get("per_page")),
		Module:     q.Get("module"),
		EntityType: q.Get("entity_type"),
		Action:     q.Get("action_type"),
		UserID:     int64Q(q.Get("user_id")),
		RecordID:   int64Q(q.Get("record_id")),
	}
}
func intQ(v string) int     { n, _ := strconv.Atoi(v); return n }
func int64Q(v string) int64 { n, _ := strconv.ParseInt(v, 10, 64); return n }
