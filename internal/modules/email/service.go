package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/messaging"
	platformemail "github.com/meherchaitanyabandaru/greenroot-api/platform/email"
)

var (
	// ErrBlocked means the email was refused before reaching the provider.
	ErrBlocked = errors.New("email blocked")
	// ErrDelivery means the provider rejected or failed the submission.
	ErrDelivery = errors.New("email delivery failed")
)

// BlockedError carries a machine-readable reason; errors.Is(err, ErrBlocked) holds.
type BlockedError struct {
	Code   string
	Reason string
}

func (e *BlockedError) Error() string        { return fmt.Sprintf("email blocked: %s (%s)", e.Reason, e.Code) }
func (e *BlockedError) Is(target error) bool { return target == ErrBlocked }

// ValidationError carries a user-facing message; errors.Is(err, ErrInvalidInput) holds.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string        { return e.Message }
func (e *ValidationError) Is(target error) bool { return target == ErrInvalidInput }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

var templateCodeRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,59}$`)

type Service struct {
	repo     Repository
	provider platformemail.Provider
	audit    *auditlog.Service
	now      func() time.Time
}

func NewService(repo Repository, provider platformemail.Provider, audit *auditlog.Service) *Service {
	return &Service{repo: repo, provider: provider, audit: audit, now: time.Now}
}

// Live reports whether email actually leaves the system.
func (s *Service) Live() bool { return s.provider.Live() }

// Name identifies email as a messaging channel.
func (s *Service) Name() string { return "EMAIL" }

// Deliver is the messaging.Channel entry point: it skips users without a
// verified email and events with no active email template.
func (s *Service) Deliver(ctx context.Context, msg messaging.Message) (messaging.Result, error) {
	if msg.Recipient.Email == "" {
		return messaging.Result{}, messaging.ErrSkipped
	}
	tpl, err := s.repo.FindTemplateByCode(ctx, msg.TemplateCode)
	if errors.Is(err, ErrNotFound) || (err == nil && !tpl.IsActive) {
		return messaging.Result{}, messaging.ErrSkipped
	}
	if err != nil {
		return messaging.Result{}, err
	}
	var userID *int64 // nil: no account yet
	if id := msg.Recipient.UserID; id != 0 {
		userID = &id
	}
	res, err := s.Send(ctx, SendRequest{
		TemplateCode:  msg.TemplateCode,
		To:            msg.Recipient.Email,
		UserID:        userID,
		Values:        msg.Values,
		Sensitive:     msg.Sensitive,
		ReferenceType: msg.ReferenceType,
		ReferenceID:   msg.ReferenceID,
	})
	return messaging.Result{MessageID: res.MessageID, Status: res.Status}, err
}

// Send renders and delivers one templated email, logging every attempt.
func (s *Service) Send(ctx context.Context, req SendRequest) (SendResult, error) {
	to, ok := NormalizeAddress(req.To)
	if !ok {
		return SendResult{}, invalid("invalid email address")
	}
	msg := Message{TemplateCode: req.TemplateCode, UserID: req.UserID, ToEmail: to, Provider: s.provider.Name(),
		ReferenceType: optional(req.ReferenceType), ReferenceID: req.ReferenceID}

	tpl, err := s.repo.FindTemplateByCode(ctx, req.TemplateCode)
	if errors.Is(err, ErrNotFound) {
		msg.Subject, msg.Body = "(template missing)", ""
		return s.block(ctx, msg, "template_missing", "email template is not configured")
	}
	if err != nil {
		return SendResult{}, err
	}
	msg.TemplateID, msg.Subject = &tpl.ID, tpl.Subject
	if !tpl.IsActive {
		return s.block(ctx, msg, "template_inactive", "email template is inactive")
	}

	sender, err := s.repo.GetSender(ctx)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return SendResult{}, err
	}
	if s.provider.Live() {
		if sender == nil || deref(sender.FromAddress) == "" {
			return s.block(ctx, msg, "sender_not_configured", "no From address is configured")
		}
		if !sender.DomainVerified {
			return s.block(ctx, msg, "domain_not_verified", "sending domain is not marked as verified (SPF/DKIM)")
		}
	}

	sensitive := map[string]bool{}
	for _, name := range req.Sensitive {
		sensitive[name] = true
	}
	rendered, err := Render(tpl.spec(), req.Values, sensitive)
	if err != nil {
		return s.block(ctx, msg, "invalid_variables", err.Error())
	}
	msg.Subject, msg.Body = rendered.MaskedSubj, rendered.MaskedText

	out := platformemail.Message{To: to, Subject: rendered.Subject, HTML: rendered.HTML, Text: rendered.Text}
	if out.HTML != "" {
		out.HTML = wrapLayout(rendered.Subject, rendered.HTML)
	}
	if sender != nil {
		out.FromName, out.From, out.ReplyTo = sender.FromName, deref(sender.FromAddress), deref(sender.ReplyTo)
	}
	res, err := s.provider.Send(ctx, out)
	if err != nil {
		msg.Status, msg.ErrorCode, msg.ErrorMessage = MessageFailed, optional("provider_error"), optional(err.Error())
		s.record(ctx, &msg)
		return SendResult{MessageID: msg.ID, Status: msg.Status}, fmt.Errorf("%w: %w", ErrDelivery, err)
	}
	now := s.now()
	msg.Status, msg.ProviderMessageID, msg.SentAt = MessageSent, optional(res.ProviderMessageID), &now
	if !s.provider.Live() {
		msg.Status = MessageMocked
	}
	s.record(ctx, &msg)
	return SendResult{MessageID: msg.ID, Status: msg.Status}, nil
}

func (s *Service) block(ctx context.Context, msg Message, code, reason string) (SendResult, error) {
	msg.Status, msg.ErrorCode, msg.ErrorMessage = MessageBlocked, &code, &reason
	s.record(ctx, &msg)
	slog.Warn("email blocked", "template_code", msg.TemplateCode, "code", code, "reason", reason)
	return SendResult{MessageID: msg.ID, Status: msg.Status}, &BlockedError{Code: code, Reason: reason}
}

// record never fails the caller: the email outcome matters more than its log row.
func (s *Service) record(ctx context.Context, msg *Message) {
	id, err := s.repo.InsertMessage(ctx, *msg)
	if err != nil {
		slog.Warn("email log insert failed", "template_code", msg.TemplateCode, "error", err)
		return
	}
	msg.ID = id
}

// ── Admin ───────────────────────────────────────────────────────────────────

func (s *Service) Status(ctx context.Context, actor ActorContext) (StatusResponse, error) {
	if !isAdmin(actor) {
		return StatusResponse{}, ErrForbidden
	}
	sender, err := s.repo.GetSender(ctx)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return StatusResponse{}, err
	}
	templates, err := s.repo.ListTemplates(ctx)
	if err != nil {
		return StatusResponse{}, err
	}
	providerDetail := "emails are logged only, never delivered"
	if s.provider.Live() {
		providerDetail = "emails are delivered"
	}
	checks := []ReadinessCheck{
		{Key: "provider", Label: "Email provider: " + s.provider.Name(), OK: s.provider.Live(), Detail: providerDetail},
		{Key: "sender", Label: "From address configured", OK: sender != nil && deref(sender.FromAddress) != ""},
		{Key: "domain", Label: "Sending domain verified (SPF, DKIM, DMARC)", OK: sender != nil && sender.DomainVerified},
	}
	for _, code := range SystemTemplateCodes {
		check := ReadinessCheck{Key: "template:" + code, Label: "Template " + code + " active"}
		idx := slices.IndexFunc(templates, func(t Template) bool { return t.TemplateCode == code })
		switch {
		case idx < 0:
			check.Detail = "missing"
		case !templates[idx].IsActive:
			check.Detail = "inactive"
		default:
			check.OK = true
		}
		checks = append(checks, check)
	}
	ready := true
	for _, c := range checks[1:] {
		ready = ready && c.OK
	}
	return StatusResponse{Provider: s.provider.Name(), Live: s.provider.Live(), Ready: ready, Checks: checks}, nil
}

func (s *Service) GetSender(ctx context.Context, actor ActorContext) (*Sender, error) {
	if !isAdmin(actor) {
		return nil, ErrForbidden
	}
	return s.repo.GetSender(ctx)
}

func (s *Service) UpdateSender(ctx context.Context, actor ActorContext, req SenderRequest) (*Sender, error) {
	if !isAdmin(actor) {
		return nil, ErrForbidden
	}
	req.FromName = strings.TrimSpace(req.FromName)
	if req.FromName == "" || len(req.FromName) > 100 || strings.ContainsAny(req.FromName, "\r\n<>\"") {
		return nil, invalid("sender name is required (max 100 characters, no < > or quotes)")
	}
	for _, field := range []**string{&req.FromAddress, &req.ReplyTo} {
		if *field == nil || strings.TrimSpace(**field) == "" {
			*field = nil
			continue
		}
		addr, ok := NormalizeAddress(**field)
		if !ok {
			return nil, invalid("%q is not a valid email address", **field)
		}
		*field = &addr
	}
	if req.DomainVerified && req.FromAddress == nil {
		return nil, invalid("set a From address before marking the domain verified")
	}
	sender, err := s.repo.UpsertSender(ctx, req)
	if err != nil {
		return nil, err
	}
	s.log(ctx, actor, "email_sender", sender.ID, auditlog.ActionUpdate, req)
	return sender, nil
}

func (s *Service) ListTemplates(ctx context.Context, actor ActorContext) ([]Template, error) {
	if !isAdmin(actor) {
		return nil, ErrForbidden
	}
	return s.repo.ListTemplates(ctx)
}

func (s *Service) GetTemplate(ctx context.Context, actor ActorContext, id int64) (*Template, error) {
	if !isAdmin(actor) {
		return nil, ErrForbidden
	}
	return s.repo.FindTemplate(ctx, id)
}

func (s *Service) CreateTemplate(ctx context.Context, actor ActorContext, req TemplateRequest) (*Template, error) {
	if !isAdmin(actor) {
		return nil, ErrForbidden
	}
	if err := normalizeTemplate(&req); err != nil {
		return nil, err
	}
	t, err := s.repo.CreateTemplate(ctx, req)
	if err != nil {
		return nil, err
	}
	s.log(ctx, actor, "email_template", t.ID, auditlog.ActionCreate, req)
	return t, nil
}

func (s *Service) UpdateTemplate(ctx context.Context, actor ActorContext, id int64, req TemplateRequest) (*Template, error) {
	if !isAdmin(actor) {
		return nil, ErrForbidden
	}
	current, err := s.repo.FindTemplate(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := normalizeTemplate(&req); err != nil {
		return nil, err
	}
	if current.IsSystem {
		if req.TemplateCode != current.TemplateCode {
			return nil, invalid("system template codes cannot be renamed; the API sends them by code")
		}
		if req.IsActive != nil && !*req.IsActive {
			return nil, invalid("system templates cannot be deactivated; the API depends on them")
		}
	}
	t, err := s.repo.UpdateTemplate(ctx, id, req)
	if err != nil {
		return nil, err
	}
	s.log(ctx, actor, "email_template", id, auditlog.ActionUpdate, req)
	return t, nil
}

func normalizeTemplate(req *TemplateRequest) error {
	req.TemplateCode = strings.ToUpper(strings.TrimSpace(req.TemplateCode))
	req.TemplateName = strings.TrimSpace(req.TemplateName)
	req.Subject = strings.TrimSpace(req.Subject)
	switch {
	case !templateCodeRe.MatchString(req.TemplateCode):
		return invalid("template code must be UPPER_SNAKE_CASE (3-60 characters)")
	case req.TemplateName == "" || len(req.TemplateName) > 255:
		return invalid("template name is required (max 255 characters)")
	}
	if errs, _ := ValidateSpec(TemplateSpec{Subject: req.Subject, BodyHTML: req.BodyHTML, BodyText: req.BodyText}); len(errs) > 0 {
		return invalid("%s", strings.Join(errs, "; "))
	}
	return nil
}

// Preview validates a draft and renders it with sample values.
func (s *Service) Preview(ctx context.Context, actor ActorContext, req PreviewRequest) (PreviewResponse, error) {
	if !isAdmin(actor) {
		return PreviewResponse{}, ErrForbidden
	}
	spec := TemplateSpec{Subject: req.Subject, BodyHTML: req.BodyHTML, BodyText: req.BodyText}
	errs, warnings := ValidateSpec(spec)
	resp := PreviewResponse{Errors: errs, Warnings: warnings, Variables: []string{}}
	if vars, err := spec.Variables(); err == nil {
		resp.Variables = vars
	}
	if len(errs) == 0 {
		if r, err := Render(spec, req.SampleValues, nil); err != nil {
			resp.Errors = append(resp.Errors, err.Error())
		} else {
			resp.Subject, resp.Text = r.Subject, r.Text
			if r.HTML != "" {
				resp.HTML = wrapLayout(r.Subject, r.HTML)
			}
		}
	}
	resp.Valid = len(resp.Errors) == 0
	if resp.Errors == nil {
		resp.Errors = []string{}
	}
	if resp.Warnings == nil {
		resp.Warnings = []string{}
	}
	return resp, nil
}

// TestSend emails a template to an admin-chosen address (real when live).
func (s *Service) TestSend(ctx context.Context, actor ActorContext, id int64, req TestSendRequest) (SendResult, error) {
	if !isAdmin(actor) {
		return SendResult{}, ErrForbidden
	}
	tpl, err := s.repo.FindTemplate(ctx, id)
	if err != nil {
		return SendResult{}, err
	}
	result, err := s.Send(ctx, SendRequest{TemplateCode: tpl.TemplateCode, To: req.To, Values: req.Values, ReferenceType: "ADMIN_TEST", ReferenceID: &actor.UserID})
	s.log(ctx, actor, "email_template", id, auditlog.ActionCreate, map[string]any{"test_send": true, "status": result.Status})
	return result, err
}

func (s *Service) ListMessages(ctx context.Context, actor ActorContext, req ListMessagesRequest) ([]Message, Pagination, error) {
	if !isAdmin(actor) {
		return nil, Pagination{}, ErrForbidden
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PerPage <= 0 || req.PerPage > 100 {
		req.PerPage = 20
	}
	req.Status, req.TemplateCode, req.ToEmail = strings.ToUpper(strings.TrimSpace(req.Status)), strings.ToUpper(strings.TrimSpace(req.TemplateCode)), strings.ToLower(strings.TrimSpace(req.ToEmail))
	items, total, err := s.repo.ListMessages(ctx, req)
	if err != nil {
		return nil, Pagination{}, err
	}
	return items, Pagination{Page: req.Page, PerPage: req.PerPage, Total: total, TotalPages: int(math.Ceil(float64(total) / float64(req.PerPage)))}, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func (s *Service) log(ctx context.Context, actor ActorContext, entityType string, entityID int64, action auditlog.Action, newValue any) {
	s.audit.Log(ctx, auditlog.Entry{
		UserID:     actor.UserID,
		Module:     auditlog.ModuleEmail,
		EntityType: entityType,
		EntityID:   entityID,
		Action:     action,
		NewValue:   newValue,
		IPAddress:  actor.IPAddress,
		DeviceInfo: actor.UserAgent,
	})
}

func isAdmin(actor ActorContext) bool { return actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") }

func optional(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
