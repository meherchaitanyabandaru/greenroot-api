package users

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/imagepolicy"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

type Handler struct {
	service  *Service
	jwt      *jwtplatform.Service
	policies *imagepolicy.Store // nil: imagepolicy.Defaults
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
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	user, err := h.service.UpdateMe(r.Context(), actor, req)
	if err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, UserResponse{User: user})
}

// CompleteOnboarding marks the user's one-time mobile onboarding as complete.
func (h *Handler) CompleteOnboarding(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CompleteOnboardingRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	user, err := h.service.CompleteOnboarding(r.Context(), actor, req)
	if err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, UserResponse{User: user})
}

// UploadAvatar handles POST /api/v1/users/me/avatar
// Accepts multipart/form-data with field "avatar" (max 5 MB).
// Uploads to MinIO profile-images bucket and updates the user's profile_image_url.
// DeleteAvatar removes the caller's profile photo and its stored file.
func (h *Handler) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	user, err := h.service.DeleteAvatar(r.Context(), actor)
	if err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, UserResponse{User: user})
}

func (h *Handler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}

	policy := imagepolicy.Defaults[imagepolicy.Selfie]
	if h.policies != nil {
		policy = h.policies.Get(r.Context(), imagepolicy.Selfie)
	}
	if err := r.ParseMultipartForm(policy.MaxBytes + 64<<10); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_form", "could not parse multipart form")
		return
	}

	file, _, err := r.FormFile("avatar")
	if err != nil {
		response.Error(w, http.StatusBadRequest, "missing_file", "avatar field is required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, policy.MaxBytes+1))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "read_error", "could not read uploaded file")
		return
	}

	// The type comes from the bytes, not the client's header: the avatar is
	// served from a public bucket. Size, sides and format follow the SELFIE policy.
	info, err := imagepolicy.Check(policy, data)
	if err != nil {
		code := "invalid_image"
		if errors.Is(err, imagepolicy.ErrTooLarge) {
			code = "file_too_large"
		}
		response.Error(w, http.StatusBadRequest, code, imagepolicy.Message(policy, err))
		return
	}
	contentType, ext := info.ContentType, strings.TrimPrefix(info.Ext, ".")

	user, err := h.service.UploadAvatar(r.Context(), actor, data, contentType, ext)
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
	if !response.DecodeJSON(w, r, &req) {
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
	if !response.DecodeJSON(w, r, &req) {
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

func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	if err := h.service.DeleteAccount(r.Context(), actor); err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, map[string]string{"message": "Account deleted. Your personal data has been removed."})
}

// SendEmailCode godoc: POST /users/me/email/send-code
func (h *Handler) SendEmailCode(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req EmailCodeRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	resp, err := h.service.SendEmailCode(r.Context(), actor, req)
	if err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, resp)
}

// VerifyEmail godoc: POST /users/me/email/verify
func (h *Handler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req VerifyEmailRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	user, err := h.service.VerifyEmail(r.Context(), actor, req)
	if err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, UserResponse{User: user})
}

// SendMobileCode godoc: POST /users/me/mobile/send-code
func (h *Handler) SendMobileCode(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req MobileCodeRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	resp, err := h.service.SendMobileCode(r.Context(), actor, req)
	if err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, resp)
}

// VerifyMobile godoc: POST /users/me/mobile/verify
func (h *Handler) VerifyMobile(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req VerifyMobileRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	user, err := h.service.VerifyMobile(r.Context(), actor, req)
	if err != nil {
		writeUsersError(w, err)
		return
	}
	response.OK(w, UserResponse{User: user})
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
	return ActorContext{
		UserID:        actor.UserID,
		Roles:         actor.Roles,
		IPAddress:     actor.IPAddress,
		UserAgent:     actor.UserAgent,
		TokenJTI:      actor.TokenJTI,
		TokenExpEpoch: actor.TokenExpEpoch,
	}, true
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
	case errors.Is(err, ErrAccountDeleted):
		response.Error(w, http.StatusGone, "account_deleted", "this account has already been deleted")
	case errors.Is(err, ErrEmailTaken):
		response.Error(w, http.StatusConflict, "email_taken", "this email is already used by another account")
	case errors.Is(err, ErrInvalidEmailCode):
		response.Error(w, http.StatusBadRequest, "invalid_code", "the verification code is wrong or has expired")
	case errors.Is(err, ErrEmailCooldown):
		response.Error(w, http.StatusTooManyRequests, "email_code_cooldown", "please wait 30 seconds before requesting another code")
	case errors.Is(err, ErrEmailRateLimited):
		response.Error(w, http.StatusTooManyRequests, "email_code_rate_limited", "too many codes requested for this email — try again in an hour")
	case errors.Is(err, ErrEmailUnavailable):
		response.Error(w, http.StatusServiceUnavailable, "email_unavailable", "could not send the email right now — please try again shortly")
	case errors.Is(err, ErrInvalidMobile):
		response.Error(w, http.StatusBadRequest, "invalid_mobile", "enter a 10-digit Indian mobile number")
	case errors.Is(err, ErrSameMobile):
		response.Error(w, http.StatusBadRequest, "same_mobile", "this is already your mobile number")
	case errors.Is(err, ErrMobileTaken):
		response.Error(w, http.StatusConflict, "mobile_taken", "this mobile number is already used by another account")
	case errors.Is(err, ErrInvalidMobileCode):
		response.Error(w, http.StatusBadRequest, "invalid_code", "the verification code is wrong or has expired")
	case errors.Is(err, ErrSMSUnavailable):
		response.Error(w, http.StatusServiceUnavailable, "sms_unavailable", "could not send the SMS right now — please try again shortly")
	case errors.Is(err, ErrAccountDeletionBlocked):
		response.Error(w, http.StatusConflict, "account_deletion_blocked", "close or transfer active nursery business before deleting this account")
	default:
		response.Error(w, http.StatusInternalServerError, "users_error", "users request failed")
	}
}
