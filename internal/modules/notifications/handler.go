package notifications

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
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	items, pagination, err := h.service.List(r.Context(), actor, listRequest(r))
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, NotificationsResponse{Notifications: items, Pagination: pagination})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.Get(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, NotificationResponse{Notification: item})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CreateNotificationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.service.Create(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, NotificationResponse{Notification: item})
}

func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	item, err := h.service.MarkRead(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, NotificationResponse{Notification: item})
}

func (h *Handler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	if err := h.service.MarkAllRead(r.Context(), actor); err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "Notifications marked as read"})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), actor, id); err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "Notification deleted"})
}

func (h *Handler) ListDevices(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	devices, err := h.service.ListDevices(r.Context(), actor)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, DevicesResponse{Devices: devices})
}

func (h *Handler) UpsertDevice(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req DeviceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	device, err := h.service.UpsertDevice(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, DeviceResponse{Device: device})
}

func (h *Handler) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.DeleteDevice(r.Context(), actor, id); err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "Device removed"})
}

func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	templates, err := h.service.ListTemplates(r.Context(), actor)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, TemplatesResponse{Templates: templates})
}

func (h *Handler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req TemplateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	t, err := h.service.CreateTemplate(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, TemplateResponse{Template: t})
}

func (h *Handler) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req TemplateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	t, err := h.service.UpdateTemplate(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, TemplateResponse{Template: t})
}

func (h *Handler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.service.DeleteTemplate(r.Context(), actor, id); err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, MessageResponse{Message: "Template deactivated"})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return ActorContext{UserID: actor.UserID, Roles: actor.Roles, IPAddress: actor.IPAddress, UserAgent: actor.UserAgent}, true
}

func listRequest(r *http.Request) ListNotificationsRequest {
	q := r.URL.Query()
	req := ListNotificationsRequest{Page: intQuery(q.Get("page")), PerPage: intQuery(q.Get("per_page")), UserID: int64Query(q.Get("user_id")), Type: q.Get("notification_type"), Status: q.Get("notification_status"), Channel: q.Get("channel"), Search: q.Get("search")}
	if q.Get("unread") != "" {
		v := q.Get("unread") == "true" || q.Get("unread") == "1"
		req.Unread = &v
	}
	return req
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
func intQuery(value string) int     { parsed, _ := strconv.Atoi(value); return parsed }
func int64Query(value string) int64 { parsed, _ := strconv.ParseInt(value, 10, 64); return parsed }

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to access notifications")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "notification resource not found")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid notification input")
	default:
		response.Error(w, http.StatusInternalServerError, "notifications_error", "notification request failed")
	}
}
