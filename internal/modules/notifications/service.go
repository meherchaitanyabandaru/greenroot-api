package notifications

import (
	"context"
	"encoding/json"
	"math"
	"strings"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
)

var (
	ErrForbidden    = apperrs.ErrForbidden
	ErrInvalidInput = apperrs.ErrInvalidInput
)

type Sender interface {
	Send(ctx context.Context, notification Notification) error
}

type MockSender struct{}

func (MockSender) Send(ctx context.Context, notification Notification) error { return nil }

type Service struct {
	repository Repository
	sender     Sender
	auditSvc   *auditlog.Service
}

func NewService(repository Repository, sender Sender, auditSvc *auditlog.Service) *Service {
	return &Service{repository: repository, sender: sender, auditSvc: auditSvc}
}

func (s *Service) List(ctx context.Context, actor ActorContext, input ListNotificationsRequest) ([]Notification, Pagination, error) {
	input = normalizeList(input)
	if !actor.HasRole("ADMIN") {
		input.UserID = actor.UserID
	}
	items, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	return items, Pagination{Page: input.Page, PerPage: input.PerPage, Total: total, TotalPages: totalPages(total, input.PerPage)}, nil
}

func (s *Service) Get(ctx context.Context, actor ActorContext, id int64) (Notification, error) {
	item, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return Notification{}, err
	}
	if err := s.canAccess(actor, *item); err != nil {
		return Notification{}, err
	}
	return *item, nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, req CreateNotificationRequest) (Notification, error) {
	if !actor.HasRole("ADMIN") {
		return Notification{}, ErrForbidden
	}
	input, err := normalizeCreate(req)
	if err != nil {
		return Notification{}, err
	}
	created, err := s.repository.Create(ctx, input)
	if err != nil {
		return Notification{}, err
	}
	_ = s.sender.Send(ctx, *created)
	s.audit(ctx, actor, "notifications", created.ID, actionInsert, req)
	return *created, nil
}

func (s *Service) MarkRead(ctx context.Context, actor ActorContext, id int64) (Notification, error) {
	current, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return Notification{}, err
	}
	if err := s.canAccess(actor, *current); err != nil {
		return Notification{}, err
	}
	updated, err := s.repository.MarkRead(ctx, id)
	if err != nil {
		return Notification{}, err
	}
	s.audit(ctx, actor, "notifications", id, actionUpdate, map[string]any{"read": true})
	return *updated, nil
}

func (s *Service) MarkAllRead(ctx context.Context, actor ActorContext) error {
	if err := s.repository.MarkAllRead(ctx, actor.UserID); err != nil {
		return err
	}
	s.audit(ctx, actor, "notifications", actor.UserID, actionUpdate, map[string]any{"read_all": true})
	return nil
}

func (s *Service) Delete(ctx context.Context, actor ActorContext, id int64) error {
	current, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.canAccess(actor, *current); err != nil {
		return err
	}
	if err := s.repository.Delete(ctx, id); err != nil {
		return err
	}
	s.audit(ctx, actor, "notifications", id, actionDelete, map[string]any{"deleted": true})
	return nil
}

func (s *Service) ListDevices(ctx context.Context, actor ActorContext) ([]Device, error) {
	return s.repository.ListDevices(ctx, actor.UserID)
}

func (s *Service) UpsertDevice(ctx context.Context, actor ActorContext, req DeviceRequest) (Device, error) {
	if strings.TrimSpace(req.FCMToken) == "" {
		return Device{}, ErrInvalidInput
	}
	device, err := s.repository.UpsertDevice(ctx, actor.UserID, req)
	if err != nil {
		return Device{}, err
	}
	s.audit(ctx, actor, "user_notification_devices", device.ID, actionInsert, req)
	return *device, nil
}

func (s *Service) DeleteDevice(ctx context.Context, actor ActorContext, id int64) error {
	if err := s.repository.DeleteDevice(ctx, id, actor.UserID, actor.HasRole("ADMIN")); err != nil {
		return err
	}
	s.audit(ctx, actor, "user_notification_devices", id, actionDelete, map[string]any{"deleted": true})
	return nil
}

func (s *Service) ListTemplates(ctx context.Context, actor ActorContext) ([]Template, error) {
	if !actor.HasRole("ADMIN") {
		return nil, ErrForbidden
	}
	return s.repository.ListTemplates(ctx)
}

func (s *Service) CreateTemplate(ctx context.Context, actor ActorContext, req TemplateRequest) (Template, error) {
	if !actor.HasRole("ADMIN") {
		return Template{}, ErrForbidden
	}
	if err := validateTemplate(req); err != nil {
		return Template{}, err
	}
	template, err := s.repository.CreateTemplate(ctx, req)
	if err != nil {
		return Template{}, err
	}
	s.audit(ctx, actor, "notification_templates", template.ID, actionInsert, req)
	return *template, nil
}

func (s *Service) UpdateTemplate(ctx context.Context, actor ActorContext, id int64, req TemplateRequest) (Template, error) {
	if !actor.HasRole("ADMIN") {
		return Template{}, ErrForbidden
	}
	if err := validateTemplate(req); err != nil {
		return Template{}, err
	}
	template, err := s.repository.UpdateTemplate(ctx, id, req)
	if err != nil {
		return Template{}, err
	}
	s.audit(ctx, actor, "notification_templates", id, actionUpdate, req)
	return *template, nil
}

func (s *Service) DeleteTemplate(ctx context.Context, actor ActorContext, id int64) error {
	if !actor.HasRole("ADMIN") {
		return ErrForbidden
	}
	if err := s.repository.DeleteTemplate(ctx, id); err != nil {
		return err
	}
	s.audit(ctx, actor, "notification_templates", id, actionDelete, map[string]any{"is_active": false})
	return nil
}

func (s *Service) canAccess(actor ActorContext, item Notification) error {
	if actor.HasRole("ADMIN") {
		return nil
	}
	if item.UserID != nil && *item.UserID == actor.UserID {
		return nil
	}
	return ErrForbidden
}

func normalizeCreate(req CreateNotificationRequest) (CreateNotificationInput, error) {
	typ := strings.ToUpper(strings.TrimSpace(req.Type))
	channel := strings.ToUpper(strings.TrimSpace(req.Channel))
	if typ == "" {
		typ = "SYSTEM"
	}
	if channel == "" {
		channel = "IN_APP"
	}
	if !isAllowedType(typ) || !isAllowedChannel(channel) || (req.UserID != nil && *req.UserID <= 0) {
		return CreateNotificationInput{}, ErrInvalidInput
	}
	data, _ := json.Marshal(req.Data)
	return CreateNotificationInput{UserID: req.UserID, Type: typ, TemplateID: req.TemplateID, Title: req.Title, Message: req.Message, Channel: channel, Status: "SENT", DataJSON: string(data)}, nil
}

func normalizeList(input ListNotificationsRequest) ListNotificationsRequest {
	if input.Page <= 0 {
		input.Page = 1
	}
	if input.PerPage <= 0 {
		input.PerPage = 20
	}
	if input.PerPage > 100 {
		input.PerPage = 100
	}
	input.Type = strings.ToUpper(strings.TrimSpace(input.Type))
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	input.Channel = strings.ToUpper(strings.TrimSpace(input.Channel))
	return input
}

func validateTemplate(req TemplateRequest) error {
	if strings.TrimSpace(req.Code) == "" || !isAllowedChannel(strings.ToUpper(strings.TrimSpace(req.Channel))) {
		return ErrInvalidInput
	}
	return nil
}

func isAllowedType(value string) bool {
	switch value {
	case "ORDER_CREATED", "ORDER_STATUS_UPDATED", "PAYMENT_RECORDED", "DISPATCH_CREATED", "DISPATCH_STATUS_UPDATED", "SUBSCRIPTION_CREATED", "SUBSCRIPTION_RENEWED", "PROMO_OFFER", "SYSTEM":
		return true
	default:
		return false
	}
}

func isAllowedChannel(value string) bool {
	switch value {
	case "PUSH", "SMS", "EMAIL", "IN_APP":
		return true
	default:
		return false
	}
}

func totalPages(total int64, perPage int) int {
	if perPage <= 0 {
		return 0
	}
	return int(math.Ceil(float64(total) / float64(perPage)))
}

func (s *Service) audit(ctx context.Context, actor ActorContext, entityType string, entityID int64, action auditlog.Action, newValue any) {
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:     actor.UserID,
		Module:     auditlog.ModuleNotifications,
		EntityType: entityType,
		EntityID:   entityID,
		Action:     action,
		NewValue:   newValue,
		IPAddress:  actor.IPAddress,
		DeviceInfo: actor.UserAgent,
	})
}
