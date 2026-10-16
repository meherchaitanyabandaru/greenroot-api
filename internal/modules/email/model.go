package email

import (
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
)

type ActorContext = authctx.ActorContext

// System template codes the API sends by code. Payment codes are shared with
// SMS so one messaging event reaches both channels.
const (
	TemplateEmailVerifyOTP              = "EMAIL_VERIFY_OTP"
	TemplatePaymentReceivedOrder        = "PAYMENT_RECEIVED_ORDER"
	TemplatePaymentReceivedSubscription = "PAYMENT_RECEIVED_SUBSCRIPTION"
	TemplatePaymentFailed               = "PAYMENT_FAILED"
)

var SystemTemplateCodes = []string{
	TemplateEmailVerifyOTP,
	TemplatePaymentReceivedOrder,
	TemplatePaymentReceivedSubscription,
	TemplatePaymentFailed,
}

// email_messages.status values.
const (
	MessageSent    = "SENT"
	MessageFailed  = "FAILED"
	MessageBlocked = "BLOCKED"
	MessageMocked  = "MOCKED"
)

type Sender struct {
	ID             int64     `json:"id"`
	FromName       string    `json:"from_name"`
	FromAddress    *string   `json:"from_address"`
	ReplyTo        *string   `json:"reply_to"`
	DomainVerified bool      `json:"domain_verified"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Template struct {
	ID           int64     `json:"id"`
	TemplateCode string    `json:"template_code"`
	TemplateName string    `json:"template_name"`
	Subject      string    `json:"subject"`
	BodyHTML     string    `json:"body_html"`
	BodyText     string    `json:"body_text"`
	Variables    []string  `json:"variables"`
	IsActive     bool      `json:"is_active"`
	IsSystem     bool      `json:"is_system"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (t Template) spec() TemplateSpec {
	return TemplateSpec{Subject: t.Subject, BodyHTML: t.BodyHTML, BodyText: t.BodyText}
}

type Message struct {
	ID                int64      `json:"id"`
	TemplateID        *int64     `json:"email_template_id"`
	TemplateCode      string     `json:"template_code"`
	UserID            *int64     `json:"user_id"`
	ToEmail           string     `json:"to_email"`
	Subject           string     `json:"subject"`
	Body              string     `json:"body"`
	Provider          string     `json:"provider"`
	ProviderMessageID *string    `json:"provider_message_id"`
	Status            string     `json:"status"`
	ErrorCode         *string    `json:"error_code"`
	ErrorMessage      *string    `json:"error_message"`
	ReferenceType     *string    `json:"reference_type"`
	ReferenceID       *int64     `json:"reference_id"`
	SentAt            *time.Time `json:"sent_at"`
	CreatedAt         time.Time  `json:"created_at"`
}

func isSystemTemplate(code string) bool {
	for _, c := range SystemTemplateCodes {
		if c == code {
			return true
		}
	}
	return false
}
