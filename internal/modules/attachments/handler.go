package attachments

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
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	items, p, err := h.service.List(r.Context(), a, listReq(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	response.OK(w, ListResponse{Attachments: items, Pagination: p})
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.Get(r.Context(), a, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	response.OK(w, AttachmentResponse{Attachment: item})
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req AttachmentRequest
	if !decode(w, r, &req) {
		return
	}
	item, err := h.service.Create(r.Context(), a, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, AttachmentResponse{Attachment: item})
}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), a, id); err != nil {
		writeErr(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "Attachment deleted"})
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
	return ListRequest{Page: intQ(q.Get("page")), PerPage: intQ(q.Get("per_page")), EntityType: q.Get("entity_type"), EntityID: int64Q(q.Get("entity_id")), Search: q.Get("search")}
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
func intQ(v string) int     { n, _ := strconv.Atoi(v); return n }
func int64Q(v string) int64 { n, _ := strconv.ParseInt(v, 10, 64); return n }
func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, 403, "forbidden", "not allowed")
	case errors.Is(err, ErrNotFound):
		response.Error(w, 404, "not_found", "attachment not found")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, 400, "invalid_input", "invalid attachment input")
	default:
		response.Error(w, 500, "attachments_error", "attachment request failed")
	}
}
