package quotations

type ListQuotationsRequest struct {
	Page            int
	PerPage         int
	Search          string
	UserID          int64
	NurseryID       int64
	BuyerNurseryID  int64
	Status          string
	SortBy          string
	SortOrder       string
	Buying          bool  // true = buyer perspective (customer_user_id or buyer_nursery_id filter)
}

type CreateQuotationRequest struct {
	QuotationType   string                  `json:"quotation_type"`  // INTERNAL or CUSTOMER
	NurseryID       *int64                  `json:"nursery_id"`
	CustomerUserID  *int64                  `json:"customer_user_id"`
	BuyerNurseryID  *int64                  `json:"buyer_nursery_id"`
	RecipientName   *string                 `json:"recipient_name"`
	RecipientMobile *string                 `json:"recipient_mobile"`
	Notes           *string                 `json:"notes"`
	Items           []QuotationItemRequest  `json:"items"`
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
	Notes           *string                `json:"notes"`
	Items           []QuotationItemRequest `json:"items"`
}

type AssignManagerRequest struct {
	ManagerUserID int64 `json:"manager_user_id"`
}

type MessageResponse struct {
	Message string `json:"message"`
}
