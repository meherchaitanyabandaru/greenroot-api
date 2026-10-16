package sms

import (
	"encoding/json"
	"errors"
	"log/slog"
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

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	status, err := h.service.Status(r.Context(), actor)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, status)
}

func (h *Handler) GetEntity(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	entity, err := h.service.GetEntity(r.Context(), actor)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"entity": entity})
}

func (h *Handler) UpdateEntity(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req EntityRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	entity, err := h.service.UpdateEntity(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"entity": entity})
}

func (h *Handler) ListHeaders(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	headers, err := h.service.ListHeaders(r.Context(), actor)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"headers": headers})
}

func (h *Handler) CreateHeader(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req HeaderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	header, err := h.service.CreateHeader(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, map[string]any{"header": header})
}

func (h *Handler) UpdateHeader(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req HeaderRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	header, err := h.service.UpdateHeader(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"header": header})
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
	response.OK(w, map[string]any{"templates": templates})
}

func (h *Handler) GetTemplate(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	template, err := h.service.GetTemplate(r.Context(), actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"template": template})
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
	template, err := h.service.CreateTemplate(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, map[string]any{"template": template})
}

func (h *Handler) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req TemplateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	template, err := h.service.UpdateTemplate(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"template": template})
}

func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req PreviewRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	preview, err := h.service.Preview(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, preview)
}

func (h *Handler) TestSend(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req TestSendRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := h.service.TestSend(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"result": result})
}

func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	items, pagination, err := h.service.ListMessages(r.Context(), actor, ListMessagesRequest{
		Page: page, PerPage: perPage, Status: q.Get("status"), TemplateCode: q.Get("template_code"), Mobile: q.Get("mobile"),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"messages": items, "pagination": pagination})
}

func (h *Handler) GetMyPreferences(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	prefs, err := h.service.GetMyPreferences(r.Context(), actor)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"preferences": prefs})
}

func (h *Handler) UpdateMyPreferences(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req PreferencesRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	prefs, err := h.service.UpdateMyPreferences(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"preferences": prefs})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return actor.AsActorContext(), true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(w, http.StatusBadRequest, "invalid_id", "invalid id")
		return 0, false
	}
	return id, true
}

func writeError(w http.ResponseWriter, err error) {
	var validation *ValidationError
	var blocked *BlockedError
	switch {
	case errors.As(err, &validation):
		response.Error(w, http.StatusBadRequest, "invalid_input", validation.Message)
	case errors.As(err, &blocked):
		response.Error(w, http.StatusUnprocessableEntity, "sms_"+blocked.Code, blocked.Reason)
	case errors.Is(err, ErrTemplateLocked):
		response.Error(w, http.StatusConflict, "sms_locked", "approved DLT content is locked; set the status back to DRAFT or SUBMITTED before editing it")
	case errors.Is(err, ErrDelivery):
		response.Error(w, http.StatusBadGateway, "sms_delivery_failed", "SMS provider rejected the message")
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to manage SMS settings")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "SMS resource not found")
	case errors.Is(err, ErrConflict):
		response.Error(w, http.StatusConflict, "conflict", "an SMS header or template with this code already exists")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid SMS input")
	default:
		slog.Error("sms request failed", "error", err)
		response.Error(w, http.StatusInternalServerError, "sms_error", "SMS request failed")
	}
}
