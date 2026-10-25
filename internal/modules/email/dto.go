package email

import "github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"

type SenderRequest struct {
	FromName       string  `json:"from_name"`
	FromAddress    *string `json:"from_address"`
	ReplyTo        *string `json:"reply_to"`
	DomainVerified bool    `json:"domain_verified"`
}

type TemplateRequest struct {
	TemplateCode string `json:"template_code"`
	TemplateName string `json:"template_name"`
	Subject      string `json:"subject"`
	BodyHTML     string `json:"body_html"`
	BodyText     string `json:"body_text"`
	IsActive     *bool  `json:"is_active"`
}

type PreviewRequest struct {
	Subject      string            `json:"subject"`
	BodyHTML     string            `json:"body_html"`
	BodyText     string            `json:"body_text"`
	SampleValues map[string]string `json:"sample_values"`
}

type PreviewResponse struct {
	Valid     bool     `json:"valid"`
	Errors    []string `json:"errors"`
	Warnings  []string `json:"warnings"`
	Variables []string `json:"variables"`
	Subject   string   `json:"subject,omitempty"`
	HTML      string   `json:"html,omitempty"`
	Text      string   `json:"text,omitempty"`
}

type TestSendRequest struct {
	To     string            `json:"to"`
	Values map[string]string `json:"values"`
}

type ListMessagesRequest struct {
	Page         int
	PerPage      int
	Status       string
	TemplateCode string
	ToEmail      string
}

type Pagination = response.Pagination

type ReadinessCheck struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type StatusResponse struct {
	Provider string           `json:"provider"`
	Live     bool             `json:"live"`
	Ready    bool             `json:"ready"`
	Checks   []ReadinessCheck `json:"checks"`
}

// SendRequest is how other modules send one templated email directly (e.g. a
// verification code to an address that is not yet on the account).
type SendRequest struct {
	TemplateCode  string
	To            string
	UserID        *int64
	Values        map[string]string
	Sensitive     []string
	ReferenceType string
	ReferenceID   *int64
}

type SendResult struct {
	MessageID int64  `json:"message_id"`
	Status    string `json:"status"`
}
