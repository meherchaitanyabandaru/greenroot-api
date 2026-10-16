package sms

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	platformsms "github.com/meherchaitanyabandaru/greenroot-api/platform/sms"
)

// ─── fakes ───────────────────────────────────────────────────────────────────

type fakeProvider struct {
	live bool
	err  error
	sent []platformsms.Message
}

func (p *fakeProvider) Name() string { return "fake" }
func (p *fakeProvider) Live() bool   { return p.live }
func (p *fakeProvider) Send(_ context.Context, msg platformsms.Message) (platformsms.Result, error) {
	if p.err != nil {
		return platformsms.Result{}, p.err
	}
	p.sent = append(p.sent, msg)
	return platformsms.Result{ProviderMessageID: "pm-1"}, nil
}

type fakeRepo struct {
	entity    *Entity
	headers   map[int64]*Header
	templates map[int64]*Template
	messages  []Message
	prefs     map[int64]Preferences
	mobiles   map[int64]string
	nextID    int64
}

func newFakeRepo() *fakeRepo {
	r := &fakeRepo{headers: map[int64]*Header{}, templates: map[int64]*Template{}, prefs: map[int64]Preferences{}, mobiles: map[int64]string{}, nextID: 100}
	r.headers[1] = &Header{ID: 1, Header: "GRROOT", HeaderType: HeaderService, RegistrationStatus: RegDraft}
	r.addTemplate(&Template{TemplateCode: TemplateAuthLoginOTP, TemplateName: "Login OTP", Category: CategoryServiceImplicit, Body: loginOTPBody, Variables: []string{"otp", "valid_minutes"}, RegistrationStatus: RegDraft, IsActive: true})
	return r
}

func (r *fakeRepo) id() int64 { r.nextID++; return r.nextID }

func (r *fakeRepo) addTemplate(t *Template) *Template {
	if t.ID == 0 {
		t.ID = r.id()
	}
	if t.HeaderID == nil {
		one := int64(1)
		t.HeaderID = &one
	}
	t.IsSystem = isSystemTemplate(t.TemplateCode)
	r.templates[t.ID] = t
	return t
}

// joined mirrors the LEFT JOIN onto sms_headers.
func (r *fakeRepo) joined(t *Template) *Template {
	c := *t
	c.Header, c.HeaderType, c.HeaderStatus = nil, nil, nil
	if t.HeaderID != nil {
		if h, ok := r.headers[*t.HeaderID]; ok {
			c.Header, c.HeaderType, c.HeaderStatus = &h.Header, &h.HeaderType, &h.RegistrationStatus
		}
	}
	return &c
}

func (r *fakeRepo) approveAll() {
	pe, tm := "1201234567890123456", "1702000000000000001"
	r.entity = &Entity{ID: 1, EntityName: "GreenRoot", PEID: &pe, TelemarketerID: &tm, RegistrationStatus: EntityApproved}
	for _, h := range r.headers {
		h.RegistrationStatus = RegApproved
	}
	for _, t := range r.templates {
		id := "1207160000000000001"
		t.RegistrationStatus, t.DLTTemplateID = RegApproved, &id
	}
}

func (r *fakeRepo) GetEntity(context.Context) (*Entity, error) {
	if r.entity == nil {
		return nil, ErrNotFound
	}
	return r.entity, nil
}
func (r *fakeRepo) UpsertEntity(_ context.Context, in EntityRequest) (*Entity, error) {
	r.entity = &Entity{ID: 1, PEID: in.PEID, EntityName: in.EntityName, DLTOperator: in.DLTOperator, TelemarketerID: in.TelemarketerID, RegistrationStatus: in.RegistrationStatus}
	return r.entity, nil
}
func (r *fakeRepo) ListHeaders(context.Context) ([]Header, error) {
	out := []Header{}
	for _, h := range r.headers {
		out = append(out, *h)
	}
	return out, nil
}
func (r *fakeRepo) FindHeader(_ context.Context, id int64) (*Header, error) {
	if h, ok := r.headers[id]; ok {
		return h, nil
	}
	return nil, ErrNotFound
}
func (r *fakeRepo) CreateHeader(_ context.Context, in HeaderRequest) (*Header, error) {
	h := &Header{ID: r.id(), Header: in.Header, HeaderType: in.HeaderType, RegistrationStatus: in.RegistrationStatus}
	r.headers[h.ID] = h
	return h, nil
}
func (r *fakeRepo) UpdateHeader(_ context.Context, id int64, in HeaderRequest) (*Header, error) {
	h := r.headers[id]
	h.Header, h.HeaderType, h.RegistrationStatus = in.Header, in.HeaderType, in.RegistrationStatus
	return h, nil
}
func (r *fakeRepo) ListTemplates(context.Context) ([]Template, error) {
	out := []Template{}
	for _, t := range r.templates {
		out = append(out, *r.joined(t))
	}
	return out, nil
}
func (r *fakeRepo) FindTemplate(_ context.Context, id int64) (*Template, error) {
	if t, ok := r.templates[id]; ok {
		return r.joined(t), nil
	}
	return nil, ErrNotFound
}
func (r *fakeRepo) FindTemplateByCode(_ context.Context, code string) (*Template, error) {
	for _, t := range r.templates {
		if t.TemplateCode == code {
			return r.joined(t), nil
		}
	}
	return nil, ErrNotFound
}
func (r *fakeRepo) CreateTemplate(_ context.Context, in TemplateRequest) (*Template, error) {
	return r.joined(r.addTemplate(fromRequest(0, in))), nil
}
func (r *fakeRepo) UpdateTemplate(_ context.Context, id int64, in TemplateRequest) (*Template, error) {
	return r.joined(r.addTemplate(fromRequest(id, in))), nil
}
func fromRequest(id int64, in TemplateRequest) *Template {
	active := in.IsActive == nil || *in.IsActive
	return &Template{ID: id, TemplateCode: in.TemplateCode, TemplateName: in.TemplateName, Category: in.Category, HeaderID: in.HeaderID,
		Body: in.Body, Variables: in.Variables, IsUnicode: in.IsUnicode, DLTTemplateID: in.DLTTemplateID,
		RegistrationStatus: in.RegistrationStatus, WhitelistedURLs: in.WhitelistedURLs, WhitelistedNumbers: in.WhitelistedNumbers, IsActive: active}
}
func (r *fakeRepo) InsertMessage(_ context.Context, m Message) (int64, error) {
	m.ID = r.id()
	r.messages = append(r.messages, m)
	return m.ID, nil
}
func (r *fakeRepo) ListMessages(context.Context, ListMessagesRequest) ([]Message, int64, error) {
	return r.messages, int64(len(r.messages)), nil
}
func (r *fakeRepo) GetPreferences(_ context.Context, userID int64) (Preferences, error) {
	if p, ok := r.prefs[userID]; ok {
		return p, nil
	}
	return Preferences{UserID: userID}, nil
}
func (r *fakeRepo) UpsertPreferences(_ context.Context, userID int64, optIn bool, source string, _ time.Time) (Preferences, error) {
	p := Preferences{UserID: userID, PromotionalOptIn: optIn, ConsentSource: &source}
	r.prefs[userID] = p
	return p, nil
}
func (r *fakeRepo) FindUserMobile(_ context.Context, userID int64) (string, error) {
	if m, ok := r.mobiles[userID]; ok {
		return m, nil
	}
	return "", ErrNotFound
}

var admin = ActorContext{UserID: 1, Roles: []string{"ADMIN"}}

func otpRequest() SendRequest {
	return SendRequest{TemplateCode: TemplateAuthLoginOTP, Mobile: "+91 98765 43210", Values: map[string]string{"otp": "482913", "valid_minutes": "5"}, Sensitive: []string{"otp"}}
}

func lastMessage(t *testing.T, r *fakeRepo) Message {
	t.Helper()
	if len(r.messages) == 0 {
		t.Fatal("no sms_messages row recorded")
	}
	return r.messages[len(r.messages)-1]
}

// ─── Send ────────────────────────────────────────────────────────────────────

func TestSend_MockModeSendsDraftTemplateAndMasksOTP(t *testing.T) {
	repo, provider := newFakeRepo(), &fakeProvider{}
	res, err := NewService(repo, provider, nil).Send(context.Background(), otpRequest())
	if err != nil {
		t.Fatalf("mock mode must allow unregistered templates: %v", err)
	}
	if res.Status != MessageMocked || len(provider.sent) != 1 {
		t.Fatalf("status=%s sent=%d", res.Status, len(provider.sent))
	}
	if provider.sent[0].To != "9876543210" || !strings.HasPrefix(provider.sent[0].Body, "482913 ") {
		t.Fatalf("provider got %+v", provider.sent[0])
	}
	if m := lastMessage(t, repo); strings.Contains(m.MessageBody, "482913") {
		t.Fatalf("OTP leaked into sms_messages: %q", m.MessageBody)
	}
}

func TestSend_LiveModeBlocksUnregistered(t *testing.T) {
	repo, provider := newFakeRepo(), &fakeProvider{live: true}
	_, err := NewService(repo, provider, nil).Send(context.Background(), otpRequest())
	var blocked *BlockedError
	if !errors.As(err, &blocked) || blocked.Code != "entity_not_approved" {
		t.Fatalf("want entity_not_approved, got %v", err)
	}
	if len(provider.sent) != 0 {
		t.Fatal("blocked SMS reached the provider")
	}
	if m := lastMessage(t, repo); m.Status != MessageBlocked || m.ErrorCode == nil {
		t.Fatalf("blocked attempt not logged: %+v", m)
	}

	repo.approveAll()
	for _, tpl := range repo.templates {
		tpl.RegistrationStatus = RegSubmitted
	}
	_, err = NewService(repo, provider, nil).Send(context.Background(), otpRequest())
	if !errors.As(err, &blocked) || blocked.Code != "template_not_approved" {
		t.Fatalf("want template_not_approved, got %v", err)
	}
}

func TestSend_LiveModeApprovedCarriesDLTIdentifiers(t *testing.T) {
	repo, provider := newFakeRepo(), &fakeProvider{live: true}
	repo.approveAll()
	res, err := NewService(repo, provider, nil).Send(context.Background(), otpRequest())
	if err != nil || res.Status != MessageSent {
		t.Fatalf("status=%s err=%v", res.Status, err)
	}
	got := provider.sent[0]
	if got.PEID != "1201234567890123456" || got.TemplateID != "1207160000000000001" || got.Header != "GRROOT" || got.TelemarketerID == "" {
		t.Fatalf("DLT identifiers missing: %+v", got)
	}
}

func TestSend_ProviderFailureIsLogged(t *testing.T) {
	repo, provider := newFakeRepo(), &fakeProvider{live: true, err: errors.New("gateway down")}
	repo.approveAll()
	_, err := NewService(repo, provider, nil).Send(context.Background(), otpRequest())
	if !errors.Is(err, ErrDelivery) {
		t.Fatalf("want ErrDelivery, got %v", err)
	}
	if m := lastMessage(t, repo); m.Status != MessageFailed {
		t.Fatalf("status = %s", m.Status)
	}
}

func TestSend_InvalidVariablesBlocked(t *testing.T) {
	repo := newFakeRepo()
	req := otpRequest()
	req.Values["otp"] = "12-34"
	_, err := NewService(repo, &fakeProvider{}, nil).Send(context.Background(), req)
	var blocked *BlockedError
	if !errors.As(err, &blocked) || blocked.Code != "invalid_variables" {
		t.Fatalf("want invalid_variables, got %v", err)
	}
}

func TestSend_PromotionalNeedsConsentAndWindow(t *testing.T) {
	repo := newFakeRepo()
	repo.headers[2] = &Header{ID: 2, Header: "123456", HeaderType: HeaderPromotional, RegistrationStatus: RegDraft}
	two := int64(2)
	repo.addTemplate(&Template{TemplateCode: "PROMO_SALE", Category: CategoryPromotional, HeaderID: &two, Body: "Plants sale on GreenRoot. Reply STOP to opt out.", Variables: []string{}, RegistrationStatus: RegDraft, IsActive: true})
	user := int64(7)
	svc := NewService(repo, &fakeProvider{}, nil)
	at := func(hour int) { svc.now = func() time.Time { return time.Date(2026, 10, 16, hour, 0, 0, 0, ist) } }
	req := SendRequest{TemplateCode: "PROMO_SALE", Mobile: "9876543210", UserID: &user}
	var blocked *BlockedError

	at(11)
	if _, err := svc.Send(context.Background(), req); !errors.As(err, &blocked) || blocked.Code != "consent_required" {
		t.Fatalf("want consent_required, got %v", err)
	}
	repo.prefs[user] = Preferences{UserID: user, PromotionalOptIn: true}
	at(21)
	if _, err := svc.Send(context.Background(), req); !errors.As(err, &blocked) || blocked.Code != "outside_promotional_window" {
		t.Fatalf("want outside_promotional_window at 21:00 IST, got %v", err)
	}
	at(9)
	if _, err := svc.Send(context.Background(), req); !errors.As(err, &blocked) {
		t.Fatalf("want block at 09:00 IST, got %v", err)
	}
	at(10)
	if _, err := svc.Send(context.Background(), req); err != nil {
		t.Fatalf("10:00 IST with consent must send, got %v", err)
	}
}

func TestSend_RejectsNonIndianMobile(t *testing.T) {
	req := otpRequest()
	req.Mobile = "12345"
	if _, err := NewService(newFakeRepo(), &fakeProvider{}, nil).Send(context.Background(), req); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
}

// ─── Admin ───────────────────────────────────────────────────────────────────

func TestAdmin_NonAdminForbidden(t *testing.T) {
	svc := NewService(newFakeRepo(), &fakeProvider{}, nil)
	buyer := ActorContext{UserID: 9, Roles: []string{"BUYER"}}
	if _, err := svc.ListTemplates(context.Background(), buyer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
	if _, err := svc.Status(context.Background(), buyer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
}

func loginTemplateRequest(repo *fakeRepo) (int64, TemplateRequest) {
	tpl, _ := repo.FindTemplateByCode(context.Background(), TemplateAuthLoginOTP)
	return tpl.ID, TemplateRequest{TemplateCode: tpl.TemplateCode, TemplateName: tpl.TemplateName, Category: tpl.Category, HeaderID: tpl.HeaderID,
		Body: tpl.Body, Variables: tpl.Variables, DLTTemplateID: tpl.DLTTemplateID, RegistrationStatus: tpl.RegistrationStatus}
}

func TestUpdateTemplate_ApprovedContentIsLocked(t *testing.T) {
	repo := newFakeRepo()
	repo.approveAll()
	svc := NewService(repo, &fakeProvider{}, nil)
	id, req := loginTemplateRequest(repo)

	req.Body = strings.Replace(req.Body, "Do not share", "Never share", 1)
	if _, err := svc.UpdateTemplate(context.Background(), admin, id, req); !errors.Is(err, ErrTemplateLocked) {
		t.Fatalf("want ErrTemplateLocked, got %v", err)
	}
	req.RegistrationStatus = RegDraft
	if _, err := svc.UpdateTemplate(context.Background(), admin, id, req); err != nil {
		t.Fatalf("editing after moving to DRAFT must work: %v", err)
	}
}

func TestUpdateTemplate_Rules(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, &fakeProvider{}, nil)
	id, base := loginTemplateRequest(repo)

	req := base
	req.RegistrationStatus = RegApproved
	if _, err := svc.UpdateTemplate(context.Background(), admin, id, req); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("APPROVED without DLT template ID must fail, got %v", err)
	}
	req = base
	req.TemplateCode = "LOGIN_OTP_V2"
	if _, err := svc.UpdateTemplate(context.Background(), admin, id, req); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("renaming a system template must fail, got %v", err)
	}
	req = base
	off := false
	req.IsActive = &off
	if _, err := svc.UpdateTemplate(context.Background(), admin, id, req); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("deactivating a system template must fail, got %v", err)
	}
	req = base
	req.Body = "Your OTP is {#var#}"
	req.Variables = []string{"otp"}
	if _, err := svc.UpdateTemplate(context.Background(), admin, id, req); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("legacy {#var#} must fail, got %v", err)
	}
}

func TestHeaderValidation(t *testing.T) {
	svc := NewService(newFakeRepo(), &fakeProvider{}, nil)
	for _, c := range []struct {
		header, typ string
		ok          bool
	}{
		{"grnrot", HeaderService, true},
		{"GRN1OT", HeaderService, false},
		{"GRROO", HeaderService, false},
		{"123456", HeaderPromotional, true},
		{"GRROOT", HeaderPromotional, false},
	} {
		_, err := svc.CreateHeader(context.Background(), admin, HeaderRequest{Header: c.header, HeaderType: c.typ})
		if (err == nil) != c.ok {
			t.Errorf("header %q/%s err=%v want ok=%v", c.header, c.typ, err, c.ok)
		}
	}
}

func TestStatus_Readiness(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, &fakeProvider{}, nil)
	status, err := svc.Status(context.Background(), admin)
	if err != nil {
		t.Fatal(err)
	}
	if status.Ready || status.Live {
		t.Fatalf("draft registry must not be ready: %+v", status)
	}
	// Only AUTH_LOGIN_OTP exists in the fake; the other system templates show as missing.
	missing := 0
	for _, c := range status.Checks {
		if c.Detail == "missing" {
			missing++
		}
	}
	if missing != len(SystemTemplateCodes)-1 {
		t.Fatalf("missing templates = %d", missing)
	}
}

func TestPreview(t *testing.T) {
	svc := NewService(newFakeRepo(), &fakeProvider{}, nil)
	resp, err := svc.Preview(context.Background(), admin, PreviewRequest{Body: loginOTPBody, Variables: []string{"otp", "valid_minutes"}, Category: CategoryServiceImplicit,
		SampleValues: map[string]string{"otp": "123456", "valid_minutes": "5"}})
	if err != nil || !resp.Valid || resp.Parts != 1 || !strings.HasPrefix(resp.Rendered, "123456 is") {
		t.Fatalf("preview = %+v, err=%v", resp, err)
	}
	resp, _ = svc.Preview(context.Background(), admin, PreviewRequest{Body: loginOTPBody, Variables: []string{"otp", "valid_minutes"}, SampleValues: map[string]string{"otp": "abc"}})
	if resp.Valid || len(resp.Errors) == 0 {
		t.Fatalf("bad sample must be invalid: %+v", resp)
	}
}

func TestPreferences(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, &fakeProvider{}, nil)
	user := ActorContext{UserID: 5, Roles: []string{"BUYER"}}
	p, _ := svc.GetMyPreferences(context.Background(), user)
	if p.PromotionalOptIn {
		t.Fatal("promotional SMS must default to opted out")
	}
	if p, err := svc.UpdateMyPreferences(context.Background(), user, PreferencesRequest{PromotionalOptIn: true}); err != nil || !p.PromotionalOptIn || *p.ConsentSource != "MOBILE_APP" {
		t.Fatalf("opt-in = %+v, err=%v", p, err)
	}
	if _, err := svc.UpdateMyPreferences(context.Background(), user, PreferencesRequest{Source: "SMS"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown source must fail, got %v", err)
	}
}
