package email

import (
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

func (h *Handler) GetSender(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	sender, err := h.service.GetSender(r.Context(), actor)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"sender": sender})
}

func (h *Handler) UpdateSender(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req SenderRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	sender, err := h.service.UpdateSender(r.Context(), actor, req)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"sender": sender})
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
	if !response.DecodeJSON(w, r, &req) {
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
	if !response.DecodeJSON(w, r, &req) {
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
	if !response.DecodeJSON(w, r, &req) {
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
	if !response.DecodeJSON(w, r, &req) {
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
		Page: page, PerPage: perPage, Status: q.Get("status"), TemplateCode: q.Get("template_code"), ToEmail: q.Get("to_email"),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"messages": items, "pagination": pagination})
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return ActorContext{}, false
	}
	return actor.AsActorContext(), true
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
		response.Error(w, http.StatusUnprocessableEntity, "email_"+blocked.Code, blocked.Reason)
	case errors.Is(err, ErrDelivery):
		response.Error(w, http.StatusBadGateway, "email_delivery_failed", "email provider rejected the message")
	case errors.Is(err, ErrForbidden):
		response.Error(w, http.StatusForbidden, "forbidden", "not allowed to manage email settings")
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", "email resource not found")
	case errors.Is(err, ErrConflict):
		response.Error(w, http.StatusConflict, "conflict", "an email template with this code already exists")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid email input")
	default:
		slog.Error("email request failed", "error", err)
		response.Error(w, http.StatusInternalServerError, "email_error", "email request failed")
	}
}
