package invites

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

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CreateInviteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	invite, err := h.service.Create(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, InviteResponse{Invite: invite})
}

func (h *Handler) GetByUUID(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	invite, err := h.service.GetByUUID(r.Context(), uuid)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, InviteResponse{Invite: invite})
}

func (h *Handler) Accept(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	uuid := chi.URLParam(r, "uuid")
	invite, err := h.service.Accept(r.Context(), actor, uuid)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, InviteResponse{Invite: invite})
}

func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	uuid := chi.URLParam(r, "uuid")
	invite, err := h.service.Cancel(r.Context(), actor, uuid)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, InviteResponse{Invite: invite})
}

func (h *Handler) ListByNursery(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	nurseryID, err := strconv.ParseInt(chi.URLParam(r, "nurseryId"), 10, 64)
	if err != nil || nurseryID <= 0 {
		response.Error(w, http.StatusBadRequest, "invalid_id", "invalid nursery id")
		return
	}
	invites, err := h.service.ListByNursery(r.Context(), actor, nurseryID)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, InvitesResponse{Invites: invites})
}

func (h *Handler) GetMyConnections(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	invites, err := h.service.ListMyConnections(r.Context(), actor)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, InvitesResponse{Invites: invites})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return ActorContext{UserID: actor.UserID, Roles: actor.Roles, IPAddress: actor.IPAddress, UserAgent: actor.UserAgent}, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to access invite")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "invite not found")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid invite input")
	case errors.Is(err, ErrConflictingRole):
		response.Error(w, http.StatusConflict, "conflicting_role", "nursery owners cannot join as managers and managers cannot become nursery owners")
	default:
		response.Error(w, http.StatusInternalServerError, "invites_error", "invite request failed")
	}
}
