package users

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

// Me godoc
//
//	@Summary		Current user profile
//	@Tags			Users
//	@Security		BearerAuth
//	@Success		200	{object}	UserResponse
//	@Router			/api/v1/users/me [get]
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	user, err := h.service.Me(r.Context(), actor)
	if err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, UserResponse{User: user})
}

// UpdateMe godoc
//
//	@Summary		Update current user profile
//	@Tags			Users
//	@Security		BearerAuth
//	@Param			request	body		UpdateProfileRequest	true	"Profile fields"
//	@Success		200		{object}	UserResponse
//	@Router			/api/v1/users/me [put]
func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req UpdateProfileRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	user, err := h.service.UpdateMe(r.Context(), actor, req)
	if err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, UserResponse{User: user})
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	userID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	user, err := h.service.GetUser(r.Context(), actor, userID)
	if err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, UserResponse{User: user})
}

func (h *Handler) ListAddresses(w http.ResponseWriter, r *http.Request) {
	actor, userID, ok := h.actorAndPathUser(w, r)
	if !ok {
		return
	}
	addresses, err := h.service.ListAddresses(r.Context(), actor, userID)
	if err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, AddressesResponse{Addresses: addresses})
}

func (h *Handler) CreateAddress(w http.ResponseWriter, r *http.Request) {
	actor, userID, ok := h.actorAndPathUser(w, r)
	if !ok {
		return
	}
	var req CreateAddressRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	address, err := h.service.CreateAddress(r.Context(), actor, userID, req)
	if err != nil {
		writeUsersError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, AddressResponse{Address: address})
}

func (h *Handler) UpdateAddress(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	addressID, ok := pathID(w, r, "addressId")
	if !ok {
		return
	}
	var req UpdateAddressRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	address, err := h.service.UpdateAddress(r.Context(), actor, addressID, req)
	if err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, AddressResponse{Address: address})
}

func (h *Handler) DeleteAddress(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	addressID, ok := pathID(w, r, "addressId")
	if !ok {
		return
	}
	if err := h.service.DeleteAddress(r.Context(), actor, addressID); err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, DeleteAddressResponse{Message: "Address deleted successfully"})
}

func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	actor, userID, ok := h.actorAndPathUser(w, r)
	if !ok {
		return
	}
	roles, err := h.service.ListRoles(r.Context(), actor, userID)
	if err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, RolesResponse{Roles: roles})
}

func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	actor, userID, ok := h.actorAndPathUser(w, r)
	if !ok {
		return
	}
	sessions, err := h.service.ListSessions(r.Context(), actor, userID)
	if err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, SessionsResponse{Sessions: sessions})
}

func (h *Handler) actorAndPathUser(w http.ResponseWriter, r *http.Request) (ActorContext, int64, bool) {
	actor, ok := h.actor(w, r)
	if !ok {
		return ActorContext{}, 0, false
	}
	userID, ok := pathID(w, r, "id")
	if !ok {
		return ActorContext{}, 0, false
	}
	return actor, userID, true
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

func pathID(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, key), 10, 64)
	if err != nil || id <= 0 {
		response.Error(w, http.StatusBadRequest, "invalid_id", "invalid id")
		return 0, false
	}
	return id, true
}

func writeUsersError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to access this user resource")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid user profile input")
	case errors.Is(err, ErrInvalidAddress):
		response.Error(w, http.StatusBadRequest, "invalid_address", "invalid address input")
	default:
		response.Error(w, http.StatusInternalServerError, "users_error", "users request failed")
	}
}
