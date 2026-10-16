package sms

type EntityRequest struct {
	PEID               *string `json:"pe_id"`
	EntityName         string  `json:"entity_name"`
	DLTOperator        string  `json:"dlt_operator"`
	TelemarketerName   *string `json:"telemarketer_name"`
	TelemarketerID     *string `json:"telemarketer_id"`
	RegistrationStatus string  `json:"registration_status"`
	Notes              *string `json:"notes"`
}

type HeaderRequest struct {
	Header             string  `json:"header"`
	HeaderType         string  `json:"header_type"`
	DLTHeaderID        *string `json:"dlt_header_id"`
	RegistrationStatus string  `json:"registration_status"`
	RejectionReason    *string `json:"rejection_reason"`
}

type TemplateRequest struct {
	TemplateCode       string   `json:"template_code"`
	TemplateName       string   `json:"template_name"`
	Category           string   `json:"category"`
	HeaderID           *int64   `json:"header_id"`
	Body               string   `json:"body"`
	Variables          []string `json:"variables"`
	IsUnicode          bool     `json:"is_unicode"`
	DLTTemplateID      *string  `json:"dlt_template_id"`
	RegistrationStatus string   `json:"registration_status"`
	RejectionReason    *string  `json:"rejection_reason"`
	WhitelistedURLs    []string `json:"whitelisted_urls"`
	WhitelistedNumbers []string `json:"whitelisted_numbers"`
	IsActive           *bool    `json:"is_active"`
}

type PreviewRequest struct {
	Body               string            `json:"body"`
	Variables          []string          `json:"variables"`
	Category           string            `json:"category"`
	HeaderID           *int64            `json:"header_id"`
	IsUnicode          bool              `json:"is_unicode"`
	WhitelistedURLs    []string          `json:"whitelisted_urls"`
	WhitelistedNumbers []string          `json:"whitelisted_numbers"`
	SampleValues       map[string]string `json:"sample_values"`
}

type PreviewResponse struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
	Rendered string   `json:"rendered,omitempty"`
	Encoding string   `json:"encoding"`
	Chars    int      `json:"chars"`
	Parts    int      `json:"parts"`
}

type TestSendRequest struct {
	Mobile string            `json:"mobile"`
	Values map[string]string `json:"values"`
}

type PreferencesRequest struct {
	PromotionalOptIn bool   `json:"promotional_opt_in"`
	Source           string `json:"source"`
}

type ListMessagesRequest struct {
	Page         int
	PerPage      int
	Status       string
	TemplateCode string
	Mobile       string
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type ReadinessCheck struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type StatusResponse struct {
	Provider  string           `json:"provider"`
	Live      bool             `json:"live"`
	Ready     bool             `json:"ready"`
	Checks    []ReadinessCheck `json:"checks"`
	Entity    *Entity          `json:"entity"`
	Templates []Template       `json:"templates"`
}

// SendRequest is how other modules ask for an SMS. Values are keyed by the
// template's variable names; Sensitive names (e.g. "otp") are masked in the log.
type SendRequest struct {
	TemplateCode  string
	Mobile        string
	UserID        *int64
	Values        map[string]string
	Sensitive     []string
	ReferenceType string
	ReferenceID   *int64
}

type SendResult struct {
	MessageID int64  `json:"message_id"`
	Status    string `json:"status"`
	Parts     int    `json:"parts"`
}
