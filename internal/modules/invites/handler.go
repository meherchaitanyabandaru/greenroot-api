package invites

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

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CreateInviteRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	invite, err := h.service.Create(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, InviteResponse{Invite: invite})
}

// Mine godoc: GET /invites/mine — open invites addressed to the caller's mobile.
func (h *Handler) Mine(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	invites, err := h.service.Mine(r.Context(), actor)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"invites": invites})
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
	return ActorContext{UserID: actor.UserID, Mobile: actor.Mobile, Roles: actor.Roles, IPAddress: actor.IPAddress, UserAgent: actor.UserAgent}, true
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
		response.Error(w, http.StatusConflict, "conflicting_role", "your current role doesn't allow you to accept this invite")
	case errors.Is(err, ErrAlreadyMember):
		response.Error(w, http.StatusConflict, "already_member", "this member is already working at another nursery")
	case errors.Is(err, ErrWrongTarget):
		response.Error(w, http.StatusForbidden, "wrong_target", "this invite was sent to a different person")
	default:
		response.Error(w, http.StatusInternalServerError, "invites_error", "invite request failed")
	}
}
