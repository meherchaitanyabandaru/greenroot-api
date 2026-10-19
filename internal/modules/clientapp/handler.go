package clientapp

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

// Handler is the HTTP transport for the clientapp module.
type Handler struct{ svc *Service }

// NewHandler returns a Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// VersionPolicy godoc: GET /app/version-policy?platform=android&version=1.4.2 (public).
func (h *Handler) VersionPolicy(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p, err := h.svc.VersionPolicy(q.Get("platform"), q.Get("version"))
	if err != nil {
		writeError(w, err)
		return
	}
	// Apps check on launch; let proxies and the client cache it briefly.
	w.Header().Set("Cache-Control", "public, max-age=300")
	response.OK(w, VersionPolicyResponse{Policy: p})
}

// SubmitCrashReport godoc: POST /app/crash-reports (public; links the user when signed in).
func (h *Handler) SubmitCrashReport(w http.ResponseWriter, r *http.Request) {
	var req CrashReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	var userID *int64
	if actor, ok := authctx.ActorFromContext(r.Context()); ok && actor.UserID > 0 {
		id := actor.UserID
		userID = &id
	}
	id, err := h.svc.SubmitCrashReport(r.Context(), userID, middleware.GetReqID(r.Context()), req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusAccepted, CrashReportAccepted{ID: id})
}

// ListCrashReports godoc: GET /admin/crash-reports (ADMIN, SUPER_ADMIN).
func (h *Handler) ListCrashReports(w http.ResponseWriter, r *http.Request) {
	actor, ok := authctx.ActorFromContext(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	items, total, err := h.svc.ListCrashReports(r.Context(), actor.HasRole("ADMIN", "SUPER_ADMIN"), ListCrashReportsRequest{
		AppVersion: q.Get("app_version"),
		Platform:   q.Get("platform"),
		FatalOnly:  q.Get("fatal") == "true",
		Page:       page,
		PerPage:    perPage,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	if items == nil {
		items = []CrashReport{}
	}
	response.OK(w, CrashReportList{Items: items, Total: total})
}

// VersionPolicyResponse wraps the policy.
type VersionPolicyResponse struct {
	Policy VersionPolicy `json:"policy"`
}

// CrashReportAccepted is returned with 202.
type CrashReportAccepted struct {
	ID int64 `json:"id"`
}

// CrashReportList is the admin listing.
type CrashReportList struct {
	Items []CrashReport `json:"items"`
	Total int64         `json:"total"`
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid client app request")
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "admin access required")
	default:
		response.Error(w, http.StatusInternalServerError, "client_app_error", "client app request failed")
	}
}
