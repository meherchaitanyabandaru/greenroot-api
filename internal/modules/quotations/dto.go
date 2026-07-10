package quotations

import "time"

type ListQuotationsRequest struct {
	Page                int
	PerPage             int
	Search              string
	UserID              int64
	NurseryID           int64
	BuyerNurseryID      int64
	Status              string
	SortBy              string
	SortOrder           string
	Buying              bool       // true = buyer perspective (customer_user_id or buyer_nursery_id filter)
	ManagerScopeUserID  int64      // non-zero = manager-only view: filter to created_by OR assigned_to this user
	UnassignedOnly      bool       // true = owner wants to see unassigned quotations only
	DateFrom            *time.Time // filter created_at >= DateFrom
	DateTo              *time.Time // filter created_at <= DateTo (end-of-day)
	AmountMin           *float64   // filter total_amount >= AmountMin
	AmountMax           *float64   // filter total_amount <= AmountMax
}

type CreateQuotationRequest struct {
	QuotationType        string                  `json:"quotation_type"`  // INTERNAL or CUSTOMER
	NurseryID            *int64                  `json:"nursery_id"`
	AssignedManagerUserID *int64                 `json:"assigned_manager_user_id"` // owner-only: pre-assign on create
	CustomerUserID       *int64                  `json:"customer_user_id"`
	BuyerNurseryID       *int64                  `json:"buyer_nursery_id"`
	RecipientName        *string                 `json:"recipient_name"`
	RecipientMobile      *string                 `json:"recipient_mobile"`
	ValidUntil           *time.Time              `json:"valid_until"`
	Notes                *string                 `json:"notes"`
	Items                []QuotationItemRequest  `json:"items"`
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
	RecipientName   *string                `json:"recipient_name"`
	RecipientMobile *string                `json:"recipient_mobile"`
	ValidUntil      *time.Time             `json:"valid_until"`
	Notes           *string                `json:"notes"`
	Items           []QuotationItemRequest `json:"items"`
}

type AssignManagerRequest struct {
	ManagerUserID int64 `json:"manager_user_id"`
}

type UnassignManagerRequest struct{}

type MessageResponse struct {
	Message string `json:"message"`
}
