package quotations

import (
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
)

type ActorContext = authctx.ActorContext


const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
	actionDelete = "DELETE"
)

type Quotation struct {
	ID              int64   `json:"id"`
	QuotationCode   string  `json:"quotation_code"`
	QuotationType   string  `json:"quotation_type"`
	CreatedByUserID int64   `json:"created_by_user_id"`
	CreatedByName   *string `json:"created_by_name,omitempty"`
	// Seller side
	NurseryID             *int64  `json:"nursery_id,omitempty"`
	NurseryName           *string `json:"nursery_name,omitempty"`
	NurseryPhone          *string `json:"nursery_phone,omitempty"`
	NurseryBrandColor     *string `json:"nursery_brand_color,omitempty"`
	AssignedManagerUserID *int64  `json:"assigned_manager_user_id,omitempty"`
	AssignedManagerName   *string `json:"assigned_manager_name,omitempty"`
	// Buyer side
	CustomerUserID  *int64  `json:"customer_user_id,omitempty"`
	BuyerNurseryID  *int64  `json:"buyer_nursery_id,omitempty"`
	RecipientName   *string `json:"recipient_name,omitempty"`
	RecipientMobile *string `json:"recipient_mobile,omitempty"`
	// Conversion
	ConvertedOrderID    *int64                       `json:"converted_order_id,omitempty"`
	ConvertedOrderCode  *string                      `json:"converted_order_code,omitempty"`
	ConvertedAt         *time.Time                   `json:"converted_at,omitempty"`
	Notes               *string                      `json:"notes,omitempty"`
	RejectionReason     *string                      `json:"rejection_reason,omitempty"`
	TotalAmount         float64                      `json:"total_amount"`
	Status              string                       `json:"status"`
	ValidUntil          *time.Time                   `json:"valid_until,omitempty"`
	SentAt              *time.Time                   `json:"sent_at,omitempty"`
	CustomerRespondedAt *time.Time                   `json:"customer_responded_at,omitempty"`
	DeletedAt           *time.Time                   `json:"deleted_at,omitempty"`
	CreatedAt           time.Time                    `json:"created_at"`
	UpdatedAt           time.Time                    `json:"updated_at"`
	Items               []QuotationItem              `json:"items,omitempty"`
	Lifecycle           *lifecycle.QuotationDisplays `json:"lifecycle,omitempty"`
	Capabilities        *QuotationCapabilities       `json:"capabilities,omitempty"`
	ExpirySummary       *QuotationExpirySummary      `json:"expiry_summary,omitempty"`
}

type QuotationCapabilities struct {
	CanEdit              bool `json:"can_edit"`
	CanUpdateCustomer    bool `json:"can_update_customer"`
	CanDelete            bool `json:"can_delete"`
	CanSend              bool `json:"can_send"`
	CanRecall            bool `json:"can_recall"`
	CanAccept            bool `json:"can_accept"`
	CanReject            bool `json:"can_reject"`
	CanConvert           bool `json:"can_convert"`
	CanAssignManager     bool `json:"can_assign_manager"`
	CanGenerateDocument  bool `json:"can_generate_document"`
	CanManageVerifyToken bool `json:"can_manage_verify_token"`
}

type QuotationExpirySummary struct {
	IsExpired     bool `json:"is_expired"`
	DaysRemaining *int `json:"days_remaining,omitempty"`
}

type QuotationItem struct {
	ID             int64     `json:"id"`
	QuotationID    int64     `json:"quotation_id"`
	PlantID        *int64    `json:"plant_id,omitempty"`
	ScientificName string    `json:"scientific_name"`
	CommonName     *string   `json:"common_name,omitempty"`
	Description    *string   `json:"description,omitempty"`
	Quantity       float64   `json:"quantity"`
	UnitPrice      float64   `json:"unit_price"`
	TotalPrice     float64   `json:"total_price"`
	CreatedAt      time.Time `json:"created_at"`
}

type QuotationVerification struct {
	VerificationID int64      `json:"verification_id"`
	QuotationID    int64      `json:"quotation_id"`
	Token          string     `json:"token"`
	Status         string     `json:"status"` // ACTIVE | REVOKED
	CreatedAt      time.Time  `json:"created_at"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	RevokedBy      *int64     `json:"revoked_by,omitempty"`
}

type QuotationDocument struct {
	DocID           int64     `json:"doc_id"`
	QuotationID     int64     `json:"quotation_id"`
	Version         int       `json:"version"`
	ObjectKey       string    `json:"object_key"`
	SHA256Hash      string    `json:"sha256_hash"`
	MimeType        string    `json:"mime_type"`
	FileSize        int64     `json:"file_size"`
	GeneratedBy     *int64    `json:"generated_by,omitempty"`
	GeneratedByName *string   `json:"generated_by_name,omitempty"`
	IsCurrent       bool      `json:"is_current"`
	CreatedAt       time.Time `json:"created_at"`
}
