package quotations

import "time"

type ListQuotationsRequest struct {
	Page               int
	PerPage            int
	Search             string
	UserID             int64
	NurseryID          int64
	BuyerNurseryID     int64
	Status             string
	SortBy             string
	SortOrder          string
	Buying             bool       // true = buyer perspective (customer_user_id or buyer_nursery_id filter)
	ManagerScopeUserID int64      // non-zero = manager-only view: filter to created_by OR assigned_to this user
	UnassignedOnly     bool       // true = owner wants to see unassigned quotations only
	DateFrom           *time.Time // filter created_at >= DateFrom
	DateTo             *time.Time // filter created_at <= DateTo (end-of-day)
	AmountMin          *float64   // filter total_amount >= AmountMin
	AmountMax          *float64   // filter total_amount <= AmountMax
}

type CreateQuotationRequest struct {
	QuotationType         string                 `json:"quotation_type"` // INTERNAL or CUSTOMER
	NurseryID             *int64                 `json:"nursery_id"`
	AssignedManagerUserID *int64                 `json:"assigned_manager_user_id"` // owner-only: pre-assign on create
	CustomerUserID        *int64                 `json:"customer_user_id"`
	BuyerNurseryID        *int64                 `json:"buyer_nursery_id"`
	RecipientName         *string                `json:"recipient_name"`
	RecipientMobile       *string                `json:"recipient_mobile"`
	ValidUntil            *time.Time             `json:"valid_until"`
	Notes                 *string                `json:"notes"`
	Items                 []QuotationItemRequest `json:"items"`
}

type AcceptRejectQuotationRequest struct {
	Reason *string `json:"reason"`
}

type QuotationItemRequest struct {
	PlantID     int64   `json:"plant_id"`
	Description *string `json:"description"`
	Quantity    float64 `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	TotalPrice  float64 `json:"total_price"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type QuotationsResponse struct {
	Quotations []Quotation `json:"quotations"`
	Pagination Pagination  `json:"pagination"`
}

type QuotationResponse struct {
	Quotation Quotation `json:"quotation"`
}

type UpdateQuotationRequest struct {
	CustomerUserID  *int64                 `json:"customer_user_id"`
	RecipientName   *string                `json:"recipient_name"`
	RecipientMobile *string                `json:"recipient_mobile"`
	ValidUntil      *time.Time             `json:"valid_until"`
	Notes           *string                `json:"notes"`
	Items           []QuotationItemRequest `json:"items"`
}

type UpdateQuotationCustomerRequest struct {
	CustomerUserID  *int64  `json:"customer_user_id"`
	RecipientName   *string `json:"recipient_name"`
	RecipientMobile *string `json:"recipient_mobile"`
}

type AssignManagerRequest struct {
	ManagerUserID int64 `json:"manager_user_id"`
}

type UnassignManagerRequest struct{}

type MessageResponse struct {
	Message string `json:"message"`
}

// VerifyTokenResponse is returned when an owner/manager requests the QR token.
type VerifyTokenResponse struct {
	Token     string    `json:"token"`
	VerifyURL string    `json:"verify_url"` // full public URL to embed in QR code
	CreatedAt time.Time `json:"created_at"`
}

// PublicVerifyResponse is the response for the unauthenticated public verification endpoint.
// Never includes nursery name, customer details, prices, or SHA-256 hash.
type PublicVerifyResponse struct {
	QuotationCode     string     `json:"quotation_code"`
	Authenticity      string     `json:"authenticity"`       // VERIFIED | INVALID
	QuotationStatus   string     `json:"quotation_status"`   // ACTIVE | EXPIRED | CANCELLED | CONVERTED
	DocumentIntegrity string     `json:"document_integrity"` // UNMODIFIED | UNVERIFIED
	IssuedAt          time.Time  `json:"issued_at"`
	ValidUntil        *time.Time `json:"valid_until,omitempty"`
	VerifiedAt        time.Time  `json:"verified_at"`
}

type DocumentResponse struct {
	Document    QuotationDocument `json:"document"`
	DownloadURL string            `json:"download_url"`
}

type DocumentsResponse struct {
	Documents []QuotationDocument `json:"documents"`
}
