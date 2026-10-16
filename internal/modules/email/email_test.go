package email

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/messaging"
	platformemail "github.com/meherchaitanyabandaru/greenroot-api/platform/email"
)

// ─── render ──────────────────────────────────────────────────────────────────

var otpSpec = TemplateSpec{
	Subject:  "Verify your email for GreenRoot",
	BodyHTML: "<p>Code: <b>{{otp}}</b>, valid {{ valid_minutes }} min</p>",
	BodyText: "Code: {{otp}}, valid {{valid_minutes}} min",
}

func TestVariables(t *testing.T) {
	vars, err := TemplateSpec{Subject: "Order {{order_code}}", BodyHTML: "{{amount}} {{order_code}}"}.Variables()
	if err != nil || strings.Join(vars, ",") != "amount,order_code" {
		t.Fatalf("vars=%v err=%v", vars, err)
	}
	if _, err := (TemplateSpec{Subject: "Hi {{Name}}"}).Variables(); err == nil {
		t.Fatal("non snake_case placeholder must be rejected")
	}
}

func TestRender_EscapesHTMLAndMasks(t *testing.T) {
	r, err := Render(otpSpec, map[string]string{"otp": "482913", "valid_minutes": "10"}, map[string]bool{"otp": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.HTML, "<b>482913</b>") || !strings.Contains(r.Text, "482913") {
		t.Fatalf("render = %+v", r)
	}
	if strings.Contains(r.MaskedText, "482913") || !strings.Contains(r.MaskedText, "******") {
		t.Fatalf("OTP leaked into log body: %q", r.MaskedText)
	}

	evil := TemplateSpec{Subject: "Order {{order_code}}", BodyHTML: "<p>{{name}}</p>", BodyText: "{{name}}"}
	r, err = Render(evil, map[string]string{"order_code": "ORD-1", "name": `<script>alert(1)</script>`}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.HTML, "<script>") || !strings.Contains(r.HTML, "&lt;script&gt;") {
		t.Fatalf("value not HTML-escaped: %q", r.HTML)
	}
	if r.Text != `<script>alert(1)</script>` {
		t.Fatalf("text body must stay raw: %q", r.Text)
	}
}

func TestRender_RejectsHeaderInjectionAndMissingValues(t *testing.T) {
	spec := TemplateSpec{Subject: "Order {{order_code}}", BodyText: "x"}
	if _, err := Render(spec, map[string]string{"order_code": "ORD-1\r\nBcc: victim@example.com"}, nil); err == nil {
		t.Fatal("CRLF in a subject value must be rejected")
	}
	if _, err := Render(spec, map[string]string{}, nil); err == nil {
		t.Fatal("missing value must be rejected")
	}
}

func TestValidateSpec(t *testing.T) {
	for name, c := range map[string]struct {
		spec TemplateSpec
		want string
	}{
		"no subject":      {TemplateSpec{BodyText: "x"}, "subject is required"},
		"no body":         {TemplateSpec{Subject: "x"}, "body is required"},
		"multiline":       {TemplateSpec{Subject: "a\nb", BodyText: "x"}, "single line"},
		"script":          {TemplateSpec{Subject: "x", BodyHTML: "<script>x</script>"}, "scripts"},
		"bad placeholder": {TemplateSpec{Subject: "x", BodyText: "{{Bad-Name}}"}, "lower_snake_case"},
	} {
		errs, _ := ValidateSpec(c.spec)
		if !strings.Contains(strings.Join(errs, "; "), c.want) {
			t.Errorf("%s: errors %v, want %q", name, errs, c.want)
		}
	}
	if errs, _ := ValidateSpec(otpSpec); len(errs) != 0 {
		t.Fatalf("valid spec rejected: %v", errs)
	}
}

func TestNormalizeAddress(t *testing.T) {
	for in, want := range map[string]string{" Ravi@Example.IN ": "ravi@example.in", "a.b+c@sub.domain.co": "a.b+c@sub.domain.co"} {
		if got, ok := NormalizeAddress(in); !ok || got != want {
			t.Errorf("NormalizeAddress(%q) = %q, %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "plain", "a@b", "Ravi <a@b.in>", "a@b.in\r\nBcc: x@y.in", "a b@c.in"} {
		if _, ok := NormalizeAddress(bad); ok {
			t.Errorf("NormalizeAddress(%q) should fail", bad)
		}
	}
}

// ─── service ─────────────────────────────────────────────────────────────────

type fakeProvider struct {
	live bool
	err  error
	sent []platformemail.Message
}

func (p *fakeProvider) Name() string { return "fake" }
func (p *fakeProvider) Live() bool   { return p.live }
func (p *fakeProvider) Send(_ context.Context, m platformemail.Message) (platformemail.Result, error) {
	if p.err != nil {
		return platformemail.Result{}, p.err
	}
	p.sent = append(p.sent, m)
	return platformemail.Result{ProviderMessageID: "pm-1"}, nil
}

type fakeRepo struct {
	sender    *Sender
	templates map[string]*Template
	messages  []Message
	nextID    int64
}

func newFakeRepo() *fakeRepo {
	r := &fakeRepo{templates: map[string]*Template{}, nextID: 10}
	r.put(&Template{TemplateCode: TemplateEmailVerifyOTP, TemplateName: "Verify", Subject: otpSpec.Subject, BodyHTML: otpSpec.BodyHTML, BodyText: otpSpec.BodyText, IsActive: true})
	return r
}

func (r *fakeRepo) put(t *Template) *Template {
	if t.ID == 0 {
		r.nextID++
		t.ID = r.nextID
	}
	t.IsSystem = isSystemTemplate(t.TemplateCode)
	r.templates[t.TemplateCode] = t
	return t
}

func (r *fakeRepo) GetSender(context.Context) (*Sender, error) {
	if r.sender == nil {
		return nil, ErrNotFound
	}
	return r.sender, nil
}
func (r *fakeRepo) UpsertSender(_ context.Context, in SenderRequest) (*Sender, error) {
	r.sender = &Sender{ID: 1, FromName: in.FromName, FromAddress: in.FromAddress, ReplyTo: in.ReplyTo, DomainVerified: in.DomainVerified, UpdatedAt: time.Now()}
	return r.sender, nil
}
func (r *fakeRepo) ListTemplates(context.Context) ([]Template, error) {
	out := []Template{}
	for _, t := range r.templates {
		out = append(out, *t)
	}
	return out, nil
}
func (r *fakeRepo) FindTemplate(_ context.Context, id int64) (*Template, error) {
	for _, t := range r.templates {
		if t.ID == id {
			c := *t
			return &c, nil
		}
	}
	return nil, ErrNotFound
}
func (r *fakeRepo) FindTemplateByCode(_ context.Context, code string) (*Template, error) {
	if t, ok := r.templates[code]; ok {
		c := *t
		return &c, nil
	}
	return nil, ErrNotFound
}
func (r *fakeRepo) CreateTemplate(_ context.Context, in TemplateRequest) (*Template, error) {
	return r.put(&Template{TemplateCode: in.TemplateCode, TemplateName: in.TemplateName, Subject: in.Subject, BodyHTML: in.BodyHTML, BodyText: in.BodyText, IsActive: activeOr(in.IsActive)}), nil
}
func (r *fakeRepo) UpdateTemplate(_ context.Context, id int64, in TemplateRequest) (*Template, error) {
	for code, t := range r.templates {
		if t.ID == id {
			delete(r.templates, code)
		}
	}
	return r.put(&Template{ID: id, TemplateCode: in.TemplateCode, TemplateName: in.TemplateName, Subject: in.Subject, BodyHTML: in.BodyHTML, BodyText: in.BodyText, IsActive: activeOr(in.IsActive)}), nil
}
func (r *fakeRepo) InsertMessage(_ context.Context, m Message) (int64, error) {
	r.nextID++
	m.ID = r.nextID
	r.messages = append(r.messages, m)
	return m.ID, nil
}
func (r *fakeRepo) ListMessages(context.Context, ListMessagesRequest) ([]Message, int64, error) {
	return r.messages, int64(len(r.messages)), nil
}

var admin = ActorContext{UserID: 1, Roles: []string{"SUPER_ADMIN"}}

func otpSend() SendRequest {
	return SendRequest{TemplateCode: TemplateEmailVerifyOTP, To: "Ravi@Example.in", Values: map[string]string{"otp": "482913", "valid_minutes": "10"}, Sensitive: []string{"otp"}}
}

func TestSend_MockLogsMaskedAndWrapsLayout(t *testing.T) {
	repo, provider := newFakeRepo(), &fakeProvider{}
	res, err := NewService(repo, provider, nil).Send(context.Background(), otpSend())
	if err != nil || res.Status != MessageMocked {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	got := provider.sent[0]
	if got.To != "ravi@example.in" || !strings.Contains(got.HTML, "<!DOCTYPE html>") || !strings.Contains(got.HTML, "482913") {
		t.Fatalf("provider got %+v", got)
	}
	if logged := repo.messages[0]; strings.Contains(logged.Body, "482913") || logged.ToEmail != "ravi@example.in" {
		t.Fatalf("log row = %+v", logged)
	}
}

func TestSend_LiveRequiresVerifiedSender(t *testing.T) {
	repo, provider := newFakeRepo(), &fakeProvider{live: true}
	svc := NewService(repo, provider, nil)
	var blocked *BlockedError

	if _, err := svc.Send(context.Background(), otpSend()); !errors.As(err, &blocked) || blocked.Code != "sender_not_configured" {
		t.Fatalf("want sender_not_configured, got %v", err)
	}
	from := "no-reply@greenroot.in"
	repo.sender = &Sender{FromName: "GreenRoot", FromAddress: &from}
	if _, err := svc.Send(context.Background(), otpSend()); !errors.As(err, &blocked) || blocked.Code != "domain_not_verified" {
		t.Fatalf("want domain_not_verified, got %v", err)
	}
	repo.sender.DomainVerified = true
	res, err := svc.Send(context.Background(), otpSend())
	if err != nil || res.Status != MessageSent || provider.sent[0].From != from {
		t.Fatalf("res=%+v err=%v sent=%+v", res, err, provider.sent)
	}
	if len(provider.sent) != 1 {
		t.Fatal("blocked emails reached the provider")
	}
}

func TestSend_ProviderFailureLogged(t *testing.T) {
	repo := newFakeRepo()
	_, err := NewService(repo, &fakeProvider{err: errors.New("smtp down")}, nil).Send(context.Background(), otpSend())
	if !errors.Is(err, ErrDelivery) || repo.messages[0].Status != MessageFailed {
		t.Fatalf("err=%v log=%+v", err, repo.messages)
	}
}

func TestDeliver_SkipsWithoutAddressOrTemplate(t *testing.T) {
	repo, provider := newFakeRepo(), &fakeProvider{}
	svc := NewService(repo, provider, nil)
	ctx := context.Background()

	if _, err := svc.Deliver(ctx, messaging.Message{TemplateCode: TemplateEmailVerifyOTP, Recipient: messaging.Recipient{UserID: 1, Mobile: "9876543210"}}); !errors.Is(err, messaging.ErrSkipped) {
		t.Fatalf("no verified email must skip, got %v", err)
	}
	if _, err := svc.Deliver(ctx, messaging.Message{TemplateCode: "ACCOUNT_WELCOME", Recipient: messaging.Recipient{UserID: 1, Email: "a@b.in"}}); !errors.Is(err, messaging.ErrSkipped) {
		t.Fatalf("event with no email template must skip, got %v", err)
	}
	if len(repo.messages) != 0 || len(provider.sent) != 0 {
		t.Fatal("skips must not log or send")
	}
	res, err := svc.Deliver(ctx, messaging.Message{TemplateCode: TemplateEmailVerifyOTP, Recipient: messaging.Recipient{UserID: 1, Email: "a@b.in"}, Values: map[string]string{"otp": "1", "valid_minutes": "1"}})
	if err != nil || res.Status != MessageMocked {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestAdminRules(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, &fakeProvider{}, nil)
	ctx := context.Background()
	buyer := ActorContext{UserID: 9, Roles: []string{"BUYER"}}
	if _, err := svc.ListTemplates(ctx, buyer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
	tpl := repo.templates[TemplateEmailVerifyOTP]
	req := TemplateRequest{TemplateCode: "OTHER_CODE", TemplateName: "x", Subject: "s", BodyText: "b"}
	if _, err := svc.UpdateTemplate(ctx, admin, tpl.ID, req); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("renaming a system template must fail, got %v", err)
	}
	off := false
	req = TemplateRequest{TemplateCode: tpl.TemplateCode, TemplateName: "x", Subject: "s", BodyText: "b", IsActive: &off}
	if _, err := svc.UpdateTemplate(ctx, admin, tpl.ID, req); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("deactivating a system template must fail, got %v", err)
	}
	if _, err := svc.CreateTemplate(ctx, admin, TemplateRequest{TemplateCode: "NEWS", TemplateName: "n", Subject: "s", BodyHTML: "<script>x</script>"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("script template must fail, got %v", err)
	}
	from := "Not An Email"
	if _, err := svc.UpdateSender(ctx, admin, SenderRequest{FromName: "GreenRoot", FromAddress: &from}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad From address must fail, got %v", err)
	}
	if _, err := svc.UpdateSender(ctx, admin, SenderRequest{FromName: "GreenRoot", DomainVerified: true}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("verified domain without From address must fail, got %v", err)
	}
}

func TestStatusAndPreview(t *testing.T) {
	svc := NewService(newFakeRepo(), &fakeProvider{}, nil)
	status, err := svc.Status(context.Background(), admin)
	if err != nil || status.Ready {
		t.Fatalf("unconfigured email must not be ready: %+v %v", status, err)
	}
	p, err := svc.Preview(context.Background(), admin, PreviewRequest{Subject: otpSpec.Subject, BodyHTML: otpSpec.BodyHTML, BodyText: otpSpec.BodyText,
		SampleValues: map[string]string{"otp": "123456", "valid_minutes": "10"}})
	if err != nil || !p.Valid || !strings.Contains(p.HTML, "123456") || strings.Join(p.Variables, ",") != "otp,valid_minutes" {
		t.Fatalf("preview = %+v %v", p, err)
	}
}
