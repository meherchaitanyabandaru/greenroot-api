package sms

import (
	"context"
	"errors"
	"fmt"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/phone"
	"log/slog"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/messaging"
	platformsms "github.com/meherchaitanyabandaru/greenroot-api/platform/sms"
)

var (
	// ErrBlocked means the SMS was refused before reaching the provider
	// (not registered, no consent, outside promo hours, bad variables).
	ErrBlocked = errors.New("sms blocked")
	// ErrDelivery means the provider rejected or failed the submission.
	ErrDelivery = errors.New("sms delivery failed")
	// ErrTemplateLocked guards DLT-approved text from silent edits.
	ErrTemplateLocked = errors.New("approved sms template is locked")
)

// BlockedError carries a machine-readable reason; errors.Is(err, ErrBlocked) holds.
type BlockedError struct {
	Code   string
	Reason string
}

func (e *BlockedError) Error() string        { return fmt.Sprintf("sms blocked: %s (%s)", e.Reason, e.Code) }
func (e *BlockedError) Is(target error) bool { return target == ErrBlocked }

// ValidationError carries a user-facing message; errors.Is(err, ErrInvalidInput) holds.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string        { return e.Message }
func (e *ValidationError) Is(target error) bool { return target == ErrInvalidInput }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

var (
	ist            = time.FixedZone("IST", 5*60*60+30*60)
	templateCodeRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,59}$`)
	dltIDRe        = regexp.MustCompile(`^[0-9]{10,30}$`)
	alphaHeaderRe  = regexp.MustCompile(`^[A-Z]{6}$`)
	numericHdrRe   = regexp.MustCompile(`^[0-9]{6}$`)
)

type Service struct {
	repo     Repository
	provider platformsms.Provider
	audit    *auditlog.Service
	now      func() time.Time
}

func NewService(repo Repository, provider platformsms.Provider, audit *auditlog.Service) *Service {
	return &Service{repo: repo, provider: provider, audit: audit, now: time.Now}
}

// Live reports whether SMS reach handsets (false for the mock provider).
func (s *Service) Live() bool { return s.provider.Live() }

// Name identifies SMS as a messaging channel.
func (s *Service) Name() string { return "SMS" }

// Deliver is the messaging.Channel entry point: it skips users without a
// mobile and events with no active SMS template, otherwise sends.
func (s *Service) Deliver(ctx context.Context, msg messaging.Message) (messaging.Result, error) {
	if msg.Recipient.Mobile == "" {
		return messaging.Result{}, messaging.ErrSkipped
	}
	tpl, err := s.repo.FindTemplateByCode(ctx, msg.TemplateCode)
	if errors.Is(err, ErrNotFound) || (err == nil && !tpl.IsActive) {
		return messaging.Result{}, messaging.ErrSkipped
	}
	if err != nil {
		return messaging.Result{}, err
	}
	userID := msg.Recipient.UserID
	res, err := s.Send(ctx, SendRequest{
		TemplateCode:  msg.TemplateCode,
		Mobile:        msg.Recipient.Mobile,
		UserID:        &userID,
		Values:        msg.Values,
		Sensitive:     msg.Sensitive,
		ReferenceType: msg.ReferenceType,
		ReferenceID:   msg.ReferenceID,
	})
	return messaging.Result{MessageID: res.MessageID, Status: res.Status}, err
}

// Send renders and delivers one templated SMS, recording every attempt
// (including blocked ones) in sms_messages.
func (s *Service) Send(ctx context.Context, req SendRequest) (SendResult, error) {
	mobile, ok := phone.Normalize(req.Mobile)
	if !ok {
		return SendResult{}, invalid("invalid Indian mobile number")
	}
	msg := Message{
		TemplateCode:  req.TemplateCode,
		UserID:        req.UserID,
		Mobile:        mobile,
		Provider:      s.provider.Name(),
		Parts:         1,
		ReferenceType: optional(req.ReferenceType),
		ReferenceID:   req.ReferenceID,
	}

	tpl, err := s.repo.FindTemplateByCode(ctx, req.TemplateCode)
	if errors.Is(err, ErrNotFound) {
		msg.Category, msg.MessageBody = "UNKNOWN", "(template missing)"
		return s.block(ctx, msg, "template_missing", "SMS template is not configured")
	}
	if err != nil {
		return SendResult{}, err
	}
	msg.TemplateID, msg.Category, msg.Header, msg.DLTTemplateID = &tpl.ID, tpl.Category, tpl.Header, tpl.DLTTemplateID
	msg.MessageBody = tpl.Body

	entity, err := s.repo.GetEntity(ctx)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return SendResult{}, err
	}
	if entity != nil {
		msg.DLTEntityID = entity.PEID
	}

	if code, reason := s.checkSendable(ctx, tpl, entity, req.UserID); code != "" {
		return s.block(ctx, msg, code, reason)
	}

	sensitive := map[string]bool{}
	for _, name := range req.Sensitive {
		sensitive[name] = true
	}
	rendered, err := Render(tpl.spec(), req.Values, sensitive)
	if err != nil {
		return s.block(ctx, msg, "invalid_variables", err.Error())
	}
	msg.MessageBody, msg.Parts = rendered.Masked, rendered.Parts

	out := platformsms.Message{To: mobile, Body: rendered.Body, Unicode: tpl.IsUnicode}
	if tpl.Header != nil {
		out.Header = *tpl.Header
	}
	if tpl.DLTTemplateID != nil {
		out.TemplateID = *tpl.DLTTemplateID
	}
	if entity != nil {
		out.PEID = deref(entity.PEID)
		out.TelemarketerID = deref(entity.TelemarketerID)
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
	return SendResult{MessageID: msg.ID, Status: msg.Status, Parts: msg.Parts}, nil
}

// checkSendable applies TRAI consent/time rules always, and DLT registration
// rules only when the provider is live (so dev works before registration).
func (s *Service) checkSendable(ctx context.Context, tpl *Template, entity *Entity, userID *int64) (code string, reason string) {
	if !tpl.IsActive || tpl.RegistrationStatus == RegInactive {
		return "template_inactive", "SMS template is inactive"
	}
	if tpl.Category == CategoryPromotional || tpl.Category == CategoryServiceExplicit {
		if userID == nil {
			return "consent_required", "consent-based SMS needs a known recipient"
		}
		prefs, err := s.repo.GetPreferences(ctx, *userID)
		if err != nil || !prefs.PromotionalOptIn {
			return "consent_required", "recipient has not opted in"
		}
	}
	if tpl.Category == CategoryPromotional {
		if h := s.now().In(ist).Hour(); h < promoWindowStartHour || h >= promoWindowEndHour {
			return "outside_promotional_window", "promotional SMS may only be sent 10:00-21:00 IST"
		}
	}
	if !s.provider.Live() {
		return "", ""
	}
	if entity == nil || entity.RegistrationStatus != EntityApproved || deref(entity.PEID) == "" {
		return "entity_not_approved", "DLT Principal Entity is not approved"
	}
	if tpl.HeaderID == nil || deref(tpl.HeaderStatus) != RegApproved {
		return "header_not_approved", "SMS header is not DLT-approved"
	}
	if !HeaderAllowsCategory(deref(tpl.HeaderType), tpl.Category) {
		return "header_category_mismatch", "header type does not allow this template category"
	}
	if tpl.RegistrationStatus != RegApproved || deref(tpl.DLTTemplateID) == "" {
		return "template_not_approved", "SMS template is not DLT-approved"
	}
	return "", ""
}

func (s *Service) block(ctx context.Context, msg Message, code, reason string) (SendResult, error) {
	msg.Status, msg.ErrorCode, msg.ErrorMessage = MessageBlocked, &code, &reason
	s.record(ctx, &msg)
	slog.Warn("sms blocked", "template_code", msg.TemplateCode, "code", code, "reason", reason)
	return SendResult{MessageID: msg.ID, Status: msg.Status}, &BlockedError{Code: code, Reason: reason}
}

// record never fails the caller: the SMS outcome matters more than its log row.
func (s *Service) record(ctx context.Context, msg *Message) {
	id, err := s.repo.InsertMessage(ctx, *msg)
	if err != nil {
		slog.Warn("sms log insert failed", "template_code", msg.TemplateCode, "error", err)
		return
	}
	msg.ID = id
}

// ── Admin: status ───────────────────────────────────────────────────────────

func (s *Service) Status(ctx context.Context, actor ActorContext) (StatusResponse, error) {
	if !isAdmin(actor) {
		return StatusResponse{}, ErrForbidden
	}
	entity, err := s.repo.GetEntity(ctx)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return StatusResponse{}, err
	}
	headers, err := s.repo.ListHeaders(ctx)
	if err != nil {
		return StatusResponse{}, err
	}
	templates, err := s.repo.ListTemplates(ctx)
	if err != nil {
		return StatusResponse{}, err
	}

	providerDetail := "messages are logged only, never delivered"
	if s.provider.Live() {
		providerDetail = "messages are delivered to handsets"
	}
	checks := []ReadinessCheck{{Key: "provider", Label: "SMS provider: " + s.provider.Name(), OK: s.provider.Live(), Detail: providerDetail}}

	entityOK := entity != nil && entity.RegistrationStatus == EntityApproved && deref(entity.PEID) != ""
	checks = append(checks, ReadinessCheck{Key: "entity", Label: "Principal Entity approved with PE ID", OK: entityOK})
	checks = append(checks, ReadinessCheck{Key: "telemarketer", Label: "Telemarketer (SMS aggregator) linked for PE-TM chain", OK: entity != nil && deref(entity.TelemarketerID) != ""})

	headerOK := slices.ContainsFunc(headers, func(h Header) bool {
		return h.HeaderType == HeaderService && h.RegistrationStatus == RegApproved
	})
	checks = append(checks, ReadinessCheck{Key: "header", Label: "SERVICE header approved", OK: headerOK})

	for _, code := range SystemTemplateCodes {
		check := ReadinessCheck{Key: "template:" + code, Label: "Template " + code + " approved"}
		idx := slices.IndexFunc(templates, func(t Template) bool { return t.TemplateCode == code })
		switch {
		case idx < 0:
			check.Detail = "missing"
		case !templates[idx].IsActive:
			check.Detail = "inactive"
		case templates[idx].RegistrationStatus != RegApproved || deref(templates[idx].DLTTemplateID) == "":
			check.Detail = "status " + templates[idx].RegistrationStatus
		case deref(templates[idx].HeaderStatus) != RegApproved:
			check.Detail = "header not approved"
		default:
			check.OK = true
		}
		checks = append(checks, check)
	}

	ready := true
	for _, c := range checks[1:] {
		ready = ready && c.OK
	}
	return StatusResponse{Provider: s.provider.Name(), Live: s.provider.Live(), Ready: ready, Checks: checks, Entity: entity, Templates: templates}, nil
}

// ── Admin: Principal Entity ─────────────────────────────────────────────────

func (s *Service) GetEntity(ctx context.Context, actor ActorContext) (*Entity, error) {
	if !isAdmin(actor) {
		return nil, ErrForbidden
	}
	return s.repo.GetEntity(ctx)
}

func (s *Service) UpdateEntity(ctx context.Context, actor ActorContext, req EntityRequest) (*Entity, error) {
	if !isAdmin(actor) {
		return nil, ErrForbidden
	}
	req.EntityName = strings.TrimSpace(req.EntityName)
	req.DLTOperator = strings.ToUpper(strings.TrimSpace(req.DLTOperator))
	req.RegistrationStatus = upperOr(req.RegistrationStatus, EntityNotRegistered)
	req.PEID, req.TelemarketerName, req.TelemarketerID, req.Notes = trimmed(req.PEID), trimmed(req.TelemarketerName), trimmed(req.TelemarketerID), trimmed(req.Notes)
	switch {
	case req.EntityName == "" || len(req.EntityName) > 255:
		return nil, invalid("entity name is required (max 255 characters)")
	case !slices.Contains([]string{"JIO", "AIRTEL", "VI", "BSNL", "TATA", "SMARTPING"}, req.DLTOperator):
		return nil, invalid("DLT operator must be one of JIO, AIRTEL, VI, BSNL, TATA, SMARTPING")
	case !slices.Contains([]string{EntityNotRegistered, EntitySubmitted, EntityApproved, EntityRejected}, req.RegistrationStatus):
		return nil, invalid("invalid registration status")
	case req.PEID != nil && !dltIDRe.MatchString(*req.PEID):
		return nil, invalid("PE ID must be 10-30 digits as shown on the DLT portal")
	case req.RegistrationStatus == EntityApproved && req.PEID == nil:
		return nil, invalid("an approved Principal Entity needs its PE ID")
	case req.TelemarketerID != nil && len(*req.TelemarketerID) > 50:
		return nil, invalid("telemarketer ID is too long")
	}
	entity, err := s.repo.UpsertEntity(ctx, req)
	if err != nil {
		return nil, err
	}
	s.log(ctx, actor, "sms_dlt_entity", entity.ID, auditlog.ActionUpdate, req)
	return entity, nil
}

// ── Admin: headers ──────────────────────────────────────────────────────────

func (s *Service) ListHeaders(ctx context.Context, actor ActorContext) ([]Header, error) {
	if !isAdmin(actor) {
		return nil, ErrForbidden
	}
	return s.repo.ListHeaders(ctx)
}

func (s *Service) CreateHeader(ctx context.Context, actor ActorContext, req HeaderRequest) (*Header, error) {
	if !isAdmin(actor) {
		return nil, ErrForbidden
	}
	if err := normalizeHeader(&req); err != nil {
		return nil, err
	}
	h, err := s.repo.CreateHeader(ctx, req)
	if err != nil {
		return nil, err
	}
	s.log(ctx, actor, "sms_header", h.ID, auditlog.ActionCreate, req)
	return h, nil
}

func (s *Service) UpdateHeader(ctx context.Context, actor ActorContext, id int64, req HeaderRequest) (*Header, error) {
	if !isAdmin(actor) {
		return nil, ErrForbidden
	}
	if err := normalizeHeader(&req); err != nil {
		return nil, err
	}
	current, err := s.repo.FindHeader(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.RegistrationStatus == RegApproved && req.RegistrationStatus == RegApproved &&
		(current.Header != req.Header || current.HeaderType != req.HeaderType) {
		return nil, ErrTemplateLocked
	}
	h, err := s.repo.UpdateHeader(ctx, id, req)
	if err != nil {
		return nil, err
	}
	s.log(ctx, actor, "sms_header", id, auditlog.ActionUpdate, req)
	return h, nil
}

func normalizeHeader(req *HeaderRequest) error {
	req.Header = strings.ToUpper(strings.TrimSpace(req.Header))
	req.HeaderType = strings.ToUpper(strings.TrimSpace(req.HeaderType))
	req.RegistrationStatus = upperOr(req.RegistrationStatus, RegDraft)
	req.DLTHeaderID, req.RejectionReason = trimmed(req.DLTHeaderID), trimmed(req.RejectionReason)
	switch req.HeaderType {
	case HeaderTransactional, HeaderService, HeaderGovernment:
		if !alphaHeaderRe.MatchString(req.Header) {
			return invalid("%s headers are exactly 6 letters (A-Z)", req.HeaderType)
		}
	case HeaderPromotional:
		if !numericHdrRe.MatchString(req.Header) {
			return invalid("PROMOTIONAL headers are exactly 6 digits")
		}
	default:
		return invalid("header type must be TRANSACTIONAL, SERVICE, PROMOTIONAL or GOVERNMENT")
	}
	if !isRegStatus(req.RegistrationStatus) {
		return invalid("invalid registration status")
	}
	if req.DLTHeaderID != nil && len(*req.DLTHeaderID) > 30 {
		return invalid("DLT header ID is too long")
	}
	return nil
}

// ── Admin: templates ────────────────────────────────────────────────────────

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
	if err := s.normalizeTemplate(ctx, &req); err != nil {
		return nil, err
	}
	t, err := s.repo.CreateTemplate(ctx, req)
	if err != nil {
		return nil, err
	}
	s.log(ctx, actor, "sms_template", t.ID, auditlog.ActionCreate, req)
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
	if err := s.normalizeTemplate(ctx, &req); err != nil {
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
	// The DLT-registered text is matched character for character at delivery;
	// editing an approved template must be a deliberate re-registration.
	if current.RegistrationStatus == RegApproved && req.RegistrationStatus == RegApproved && contentChanged(*current, req) {
		return nil, ErrTemplateLocked
	}
	t, err := s.repo.UpdateTemplate(ctx, id, req)
	if err != nil {
		return nil, err
	}
	s.log(ctx, actor, "sms_template", id, auditlog.ActionUpdate, req)
	return t, nil
}

func contentChanged(current Template, req TemplateRequest) bool {
	return current.Body != req.Body ||
		!slices.Equal(current.Variables, req.Variables) ||
		current.Category != req.Category ||
		current.IsUnicode != req.IsUnicode ||
		!equalPtr(current.HeaderID, req.HeaderID) ||
		!slices.Equal(current.WhitelistedURLs, req.WhitelistedURLs) ||
		!slices.Equal(current.WhitelistedNumbers, req.WhitelistedNumbers)
}

func (s *Service) normalizeTemplate(ctx context.Context, req *TemplateRequest) error {
	req.TemplateCode = strings.ToUpper(strings.TrimSpace(req.TemplateCode))
	req.TemplateName = strings.TrimSpace(req.TemplateName)
	req.Category = strings.ToUpper(strings.TrimSpace(req.Category))
	req.RegistrationStatus = upperOr(req.RegistrationStatus, RegDraft)
	req.DLTTemplateID, req.RejectionReason = trimmed(req.DLTTemplateID), trimmed(req.RejectionReason)
	req.Variables, req.WhitelistedURLs, req.WhitelistedNumbers = cleanList(req.Variables), cleanList(req.WhitelistedURLs), cleanList(req.WhitelistedNumbers)
	switch {
	case !templateCodeRe.MatchString(req.TemplateCode):
		return invalid("template code must be UPPER_SNAKE_CASE (3-60 characters)")
	case req.TemplateName == "" || len(req.TemplateName) > 255:
		return invalid("template name is required (max 255 characters)")
	case !slices.Contains([]string{CategoryTransactional, CategoryServiceImplicit, CategoryServiceExplicit, CategoryPromotional}, req.Category):
		return invalid("category must be TRANSACTIONAL, SERVICE_IMPLICIT, SERVICE_EXPLICIT or PROMOTIONAL")
	case !isRegStatus(req.RegistrationStatus):
		return invalid("invalid registration status")
	case req.DLTTemplateID != nil && !dltIDRe.MatchString(*req.DLTTemplateID):
		return invalid("DLT template ID must be 10-30 digits as shown on the DLT portal")
	case req.RegistrationStatus == RegApproved && req.DLTTemplateID == nil:
		return invalid("an approved template needs its DLT template ID")
	}
	spec := TemplateSpec{Body: req.Body, Variables: req.Variables, Category: req.Category, IsUnicode: req.IsUnicode, URLs: req.WhitelistedURLs, Numbers: req.WhitelistedNumbers}
	if req.HeaderID != nil {
		header, err := s.repo.FindHeader(ctx, *req.HeaderID)
		if errors.Is(err, ErrNotFound) {
			return invalid("header not found")
		}
		if err != nil {
			return err
		}
		spec.HeaderType = header.HeaderType
	}
	if errs, _ := ValidateSpec(spec); len(errs) > 0 {
		return invalid("%s", strings.Join(errs, "; "))
	}
	return nil
}

// Preview validates a draft template and renders it with sample values so
// admins can check it before submitting to DLT.
func (s *Service) Preview(ctx context.Context, actor ActorContext, req PreviewRequest) (PreviewResponse, error) {
	if !isAdmin(actor) {
		return PreviewResponse{}, ErrForbidden
	}
	spec := TemplateSpec{Body: req.Body, Variables: cleanList(req.Variables), Category: strings.ToUpper(req.Category), IsUnicode: req.IsUnicode,
		URLs: cleanList(req.WhitelistedURLs), Numbers: cleanList(req.WhitelistedNumbers)}
	if req.HeaderID != nil {
		if header, err := s.repo.FindHeader(ctx, *req.HeaderID); err == nil {
			spec.HeaderType = header.HeaderType
		}
	}
	errs, warnings := ValidateSpec(spec)
	resp := PreviewResponse{Errors: errs, Warnings: warnings, Encoding: "GSM-7"}
	if spec.IsUnicode {
		resp.Encoding = "UCS-2 (Unicode)"
	}
	if len(errs) == 0 {
		if rendered, err := Render(spec, req.SampleValues, nil); err != nil {
			resp.Errors = append(resp.Errors, err.Error())
		} else {
			resp.Rendered, resp.Chars, resp.Parts = rendered.Body, rendered.Chars, rendered.Parts
			if rendered.Parts > 1 {
				resp.Warnings = append(resp.Warnings, fmt.Sprintf("this message is billed as %d SMS parts", rendered.Parts))
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

// TestSend sends a template to an admin-chosen number. With a live provider
// this is a real, billable SMS.
func (s *Service) TestSend(ctx context.Context, actor ActorContext, id int64, req TestSendRequest) (SendResult, error) {
	if !isAdmin(actor) {
		return SendResult{}, ErrForbidden
	}
	tpl, err := s.repo.FindTemplate(ctx, id)
	if err != nil {
		return SendResult{}, err
	}
	result, err := s.Send(ctx, SendRequest{TemplateCode: tpl.TemplateCode, Mobile: req.Mobile, Values: req.Values, ReferenceType: "ADMIN_TEST", ReferenceID: &actor.UserID})
	s.log(ctx, actor, "sms_template", id, auditlog.ActionCreate, map[string]any{"test_send": true, "status": result.Status})
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
	req.Status, req.TemplateCode, req.Mobile = strings.ToUpper(strings.TrimSpace(req.Status)), strings.ToUpper(strings.TrimSpace(req.TemplateCode)), strings.TrimSpace(req.Mobile)
	items, total, err := s.repo.ListMessages(ctx, req)
	if err != nil {
		return nil, Pagination{}, err
	}
	return items, Pagination{Page: req.Page, PerPage: req.PerPage, Total: total, TotalPages: int(math.Ceil(float64(total) / float64(req.PerPage)))}, nil
}

// ── Self-service consent ────────────────────────────────────────────────────

func (s *Service) GetMyPreferences(ctx context.Context, actor ActorContext) (Preferences, error) {
	return s.repo.GetPreferences(ctx, actor.UserID)
}

func (s *Service) UpdateMyPreferences(ctx context.Context, actor ActorContext, req PreferencesRequest) (Preferences, error) {
	source := upperOr(req.Source, "MOBILE_APP")
	if !slices.Contains([]string{"MOBILE_APP", "ADMIN_UI", "WEB"}, source) {
		return Preferences{}, invalid("source must be MOBILE_APP, ADMIN_UI or WEB")
	}
	prefs, err := s.repo.UpsertPreferences(ctx, actor.UserID, req.PromotionalOptIn, source, s.now())
	if err != nil {
		return Preferences{}, err
	}
	s.log(ctx, actor, "sms_preferences", actor.UserID, auditlog.ActionUpdate, req)
	return prefs, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func (s *Service) log(ctx context.Context, actor ActorContext, entityType string, entityID int64, action auditlog.Action, newValue any) {
	s.audit.Log(ctx, auditlog.Entry{
		UserID:     actor.UserID,
		Module:     auditlog.ModuleSMS,
		EntityType: entityType,
		EntityID:   entityID,
		Action:     action,
		NewValue:   newValue,
		IPAddress:  actor.IPAddress,
		DeviceInfo: actor.UserAgent,
	})
}

func isAdmin(actor ActorContext) bool { return actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") }

func isRegStatus(v string) bool {
	return slices.Contains([]string{RegDraft, RegSubmitted, RegApproved, RegRejected, RegInactive}, v)
}

func upperOr(v, fallback string) string {
	if v = strings.ToUpper(strings.TrimSpace(v)); v != "" {
		return v
	}
	return fallback
}

func trimmed(v *string) *string {
	if v == nil {
		return nil
	}
	if t := strings.TrimSpace(*v); t != "" {
		return &t
	}
	return nil
}

func cleanList(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if t := strings.TrimSpace(item); t != "" {
			out = append(out, t)
		}
	}
	return out
}

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

func equalPtr(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
