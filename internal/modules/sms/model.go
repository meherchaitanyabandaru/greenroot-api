package sms

import (
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
)

type ActorContext = authctx.ActorContext

// System template codes the API sends on its own. Each must exist in
// sms_templates; in live mode each must also be DLT-approved.
const (
	TemplateAuthLoginOTP                = "AUTH_LOGIN_OTP"
	TemplateAuthRegisterOTP             = "AUTH_REGISTER_OTP"
	TemplateAccountWelcome              = "ACCOUNT_WELCOME"
	TemplatePaymentReceivedOrder        = "PAYMENT_RECEIVED_ORDER"
	TemplatePaymentReceivedSubscription = "PAYMENT_RECEIVED_SUBSCRIPTION"
	TemplatePaymentFailed               = "PAYMENT_FAILED"
	TemplateDeliveryUnconfirmed         = "DELIVERY_UNCONFIRMED"
	// Order lifecycle (20261028_01): see greenroot-infra docs/messaging.md.
	TemplateQuotationReceived = "QUOTATION_RECEIVED"
	TemplateOrderPlaced       = "ORDER_PLACED"
	TemplateDeliveryOnTheWay  = "DELIVERY_ON_THE_WAY"
	TemplateOrderPartial      = "ORDER_PARTIAL"
	TemplateNurseryApproved   = "NURSERY_APPROVED"
	TemplateTrialEnding       = "TRIAL_ENDING"
	TemplateDeliveryIssue     = "DELIVERY_ISSUE"
)

var SystemTemplateCodes = []string{
	TemplateAuthLoginOTP,
	TemplateAuthRegisterOTP,
	TemplateAccountWelcome,
	TemplatePaymentReceivedOrder,
	TemplatePaymentReceivedSubscription,
	TemplatePaymentFailed,
	TemplateDeliveryUnconfirmed,
	TemplateQuotationReceived,
	TemplateOrderPlaced,
	TemplateDeliveryOnTheWay,
	TemplateOrderPartial,
	TemplateNurseryApproved,
	TemplateTrialEnding,
	TemplateDeliveryIssue,
}

// DLT template categories.
const (
	CategoryTransactional   = "TRANSACTIONAL"    // banks only: OTP for a banking transaction
	CategoryServiceImplicit = "SERVICE_IMPLICIT" // OTP, order/payment alerts to existing customers; no consent needed
	CategoryServiceExplicit = "SERVICE_EXPLICIT" // service messages that need recorded consent
	CategoryPromotional     = "PROMOTIONAL"      // marketing; consent + 10:00-21:00 IST only
)

// DLT header types.
const (
	HeaderTransactional = "TRANSACTIONAL"
	HeaderService       = "SERVICE"
	HeaderPromotional   = "PROMOTIONAL"
	HeaderGovernment    = "GOVERNMENT"
)

// Registration statuses shared by headers and templates.
const (
	RegDraft     = "DRAFT"
	RegSubmitted = "SUBMITTED"
	RegApproved  = "APPROVED"
	RegRejected  = "REJECTED"
	RegInactive  = "INACTIVE"
)

// Principal Entity statuses.
const (
	EntityNotRegistered = "NOT_REGISTERED"
	EntitySubmitted     = "SUBMITTED"
	EntityApproved      = "APPROVED"
	EntityRejected      = "REJECTED"
)

// sms_messages.status values.
const (
	MessageSent    = "SENT"
	MessageFailed  = "FAILED"
	MessageBlocked = "BLOCKED"
	MessageMocked  = "MOCKED"
)

// Promotional SMS may only be delivered between these IST hours (TCCCPR).
const (
	promoWindowStartHour = 10
	promoWindowEndHour   = 21
)

type Entity struct {
	ID                 int64     `json:"id"`
	PEID               *string   `json:"pe_id"`
	EntityName         string    `json:"entity_name"`
	DLTOperator        string    `json:"dlt_operator"`
	TelemarketerName   *string   `json:"telemarketer_name"`
	TelemarketerID     *string   `json:"telemarketer_id"`
	RegistrationStatus string    `json:"registration_status"`
	Notes              *string   `json:"notes"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Header struct {
	ID                 int64     `json:"id"`
	Header             string    `json:"header"`
	HeaderType         string    `json:"header_type"`
	DLTHeaderID        *string   `json:"dlt_header_id"`
	RegistrationStatus string    `json:"registration_status"`
	RejectionReason    *string   `json:"rejection_reason"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Template struct {
	ID                 int64      `json:"id"`
	TemplateCode       string     `json:"template_code"`
	TemplateName       string     `json:"template_name"`
	Category           string     `json:"category"`
	HeaderID           *int64     `json:"header_id"`
	Header             *string    `json:"header"`
	HeaderType         *string    `json:"header_type"`
	HeaderStatus       *string    `json:"header_status"`
	Body               string     `json:"body"`
	Variables          []string   `json:"variables"`
	IsUnicode          bool       `json:"is_unicode"`
	DLTTemplateID      *string    `json:"dlt_template_id"`
	RegistrationStatus string     `json:"registration_status"`
	RejectionReason    *string    `json:"rejection_reason"`
	WhitelistedURLs    []string   `json:"whitelisted_urls"`
	WhitelistedNumbers []string   `json:"whitelisted_numbers"`
	IsActive           bool       `json:"is_active"`
	IsSystem           bool       `json:"is_system"`
	ApprovedAt         *time.Time `json:"approved_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (t Template) spec() TemplateSpec {
	spec := TemplateSpec{
		Body:      t.Body,
		Variables: t.Variables,
		Category:  t.Category,
		IsUnicode: t.IsUnicode,
		URLs:      t.WhitelistedURLs,
		Numbers:   t.WhitelistedNumbers,
	}
	if t.HeaderType != nil {
		spec.HeaderType = *t.HeaderType
	}
	return spec
}

type Message struct {
	ID                int64      `json:"id"`
	TemplateID        *int64     `json:"sms_template_id"`
	TemplateCode      string     `json:"template_code"`
	UserID            *int64     `json:"user_id"`
	Mobile            string     `json:"mobile"`
	MessageBody       string     `json:"message_body"`
	Header            *string    `json:"header"`
	DLTEntityID       *string    `json:"dlt_entity_id"`
	DLTTemplateID     *string    `json:"dlt_template_id"`
	Category          string     `json:"category"`
	Parts             int        `json:"parts"`
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

type Preferences struct {
	UserID               int64      `json:"user_id"`
	PromotionalOptIn     bool       `json:"promotional_opt_in"`
	PromotionalConsentAt *time.Time `json:"promotional_consent_at"`
	PromotionalRevokedAt *time.Time `json:"promotional_revoked_at"`
	ConsentSource        *string    `json:"consent_source"`
	UpdatedAt            *time.Time `json:"updated_at"`
}

func isSystemTemplate(code string) bool {
	for _, c := range SystemTemplateCodes {
		if c == code {
			return true
		}
	}
	return false
}
