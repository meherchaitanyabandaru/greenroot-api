package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/validator"
)

var mobilePattern = regexp.MustCompile(`^[0-9+ -]{7,20}$`)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// SendOTP godoc
//
//	@Summary		Send OTP
//	@Description	Sends a mocked OTP for mobile login. Current development OTP is 123456.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		SendOTPRequest	true	"Mobile number"
//	@Success		200		{object}	SendOTPResponse
//	@Failure		400		{object}	response.ErrorBody
//	@Router			/api/v1/auth/send-otp [post]
func (h *Handler) SendOTP(w http.ResponseWriter, r *http.Request) {
	var req SendOTPRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !validateMobile(w, req.Mobile) {
		return
	}

	result, err := h.service.SendOTP(r.Context(), req)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	response.OK(w, result)
}

// VerifyOTP godoc
//
//	@Summary		Verify OTP
//	@Description	Verifies mocked OTP, creates user if needed, creates session, writes login activity, and returns JWT tokens.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		VerifyOTPRequest	true	"OTP verification"
//	@Success		200		{object}	AuthResponse
//	@Failure		400		{object}	response.ErrorBody
//	@Failure		401		{object}	response.ErrorBody
//	@Router			/api/v1/auth/verify-otp [post]
func (h *Handler) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var req VerifyOTPRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !validateMobile(w, req.Mobile) || !validateRequired(w, "otp", req.OTP) {
		return
	}

	result, err := h.service.VerifyOTP(r.Context(), req, clientContext(r))
	if err != nil {
		writeAuthError(w, err)
		return
	}
	response.OK(w, result)
}

// RefreshToken godoc
//
//	@Summary		Refresh token
//	@Description	Rotates refresh token and returns a fresh access token.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		RefreshTokenRequest	true	"Refresh token"
//	@Success		200		{object}	AuthResponse
//	@Failure		400		{object}	response.ErrorBody
//	@Failure		401		{object}	response.ErrorBody
//	@Router			/api/v1/auth/refresh-token [post]
func (h *Handler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var req RefreshTokenRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !validateRequired(w, "refresh_token", req.RefreshToken) {
		return
	}

	result, err := h.service.RefreshToken(r.Context(), req)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	response.OK(w, result)
}

// Logout godoc
//
//	@Summary		Logout
//	@Description	Logs out the current refresh-token backed session.
//	@Tags			Auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		LogoutRequest	false	"Refresh token. Authorization bearer token is also accepted."
//	@Success		200		{object}	LogoutResponse
//	@Failure		401		{object}	response.ErrorBody
//	@Router			/api/v1/auth/logout [post]
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req LogoutRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	refreshToken := strings.TrimSpace(req.RefreshToken)
	accessToken := bearerToken(r)

	if err := h.service.Logout(r.Context(), refreshToken, accessToken); err != nil {
		writeAuthError(w, err)
		return
	}

	response.OK(w, LogoutResponse{Message: "Logged out successfully"})
}

// Me godoc
//
//	@Summary		Current user
//	@Description	Returns the authenticated user for the access token.
//	@Tags			Auth
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	MeResponse
//	@Failure		401	{object}	response.ErrorBody
//	@Router			/api/v1/auth/me [get]
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		response.Error(w, http.StatusUnauthorized, "missing_token", "missing bearer token")
		return
	}

	user, err := h.service.Me(r.Context(), token)
	if err != nil {
		writeAuthError(w, err)
		return
	}

	response.OK(w, MeResponse{User: user})
}

// Workspaces godoc
//
//	@Summary		List workspaces
//	@Description	Returns all workspaces the authenticated user can operate in (personal, owned nursery, manager nurseries, driver).
//	@Tags			Auth
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{array}		Workspace
//	@Failure		401	{object}	response.ErrorBody
//	@Router			/api/v1/me/workspaces [get]
func (h *Handler) Workspaces(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		response.Error(w, http.StatusUnauthorized, "missing_token", "missing bearer token")
		return
	}

	workspaces, err := h.service.Workspaces(r.Context(), token)
	if err != nil {
		writeAuthError(w, err)
		return
	}

	response.OK(w, workspaces)
}

func (h *Handler) OwnerDashboard(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		response.Error(w, http.StatusUnauthorized, "missing_token", "missing bearer token")
		return
	}
	dashboard, err := h.service.OwnerDashboard(r.Context(), token)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	response.OK(w, map[string]any{"dashboard": dashboard})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return false
	}
	return true
}

func validateMobile(w http.ResponseWriter, mobile string) bool {
	v := validator.New()
	v.Required("mobile", mobile)
	if v.Valid() && !mobilePattern.MatchString(mobile) {
		response.Error(w, http.StatusBadRequest, "invalid_mobile", "mobile must be 7-20 characters and contain digits, spaces, + or - only")
		return false
	}
	if !v.Valid() {
		response.JSON(w, http.StatusBadRequest, response.Envelope{"errors": v.Errors()})
		return false
	}
	return true
}

func validateRequired(w http.ResponseWriter, field string, value string) bool {
	v := validator.New()
	v.Required(field, value)
	if !v.Valid() {
		response.JSON(w, http.StatusBadRequest, response.Envelope{"errors": v.Errors()})
		return false
	}
	return true
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
}

func clientContext(r *http.Request) ClientContext {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return ClientContext{
		IPAddress: host,
		UserAgent: r.UserAgent(),
	}
}

func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidOTP):
		response.Error(w, http.StatusUnauthorized, "invalid_otp", "invalid OTP")
	case errors.Is(err, ErrInvalidRefreshToken):
		response.Error(w, http.StatusUnauthorized, "invalid_refresh_token", "invalid refresh token")
	case errors.Is(err, ErrInvalidToken):
		response.Error(w, http.StatusUnauthorized, "invalid_token", "invalid access token")
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed")
	case errors.Is(err, ErrUserNotFound):
		response.Error(w, http.StatusNotFound, "user_not_found", "user not found")
	default:
		slog.Error("auth request failed", "error", err)
		response.Error(w, http.StatusInternalServerError, "auth_error", "authentication request failed")
	}
}
