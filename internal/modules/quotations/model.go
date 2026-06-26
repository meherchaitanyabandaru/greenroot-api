package quotations

import "time"

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
	actionDelete = "DELETE"
)

type Quotation struct {
	ID                      int64           `json:"id"`
	QuotationCode           string          `json:"quotation_code"`
	QuotationType           string          `json:"quotation_type"`
	CreatedByUserID         int64           `json:"created_by_user_id"`
	CreatedByName           *string         `json:"created_by_name,omitempty"`
	// Seller side
	NurseryID               *int64          `json:"nursery_id,omitempty"`
	NurseryName             *string         `json:"nursery_name,omitempty"`
	NurseryPhone            *string         `json:"nursery_phone,omitempty"`
	AssignedManagerUserID   *int64          `json:"assigned_manager_user_id,omitempty"`
	// Buyer side
	CustomerUserID          *int64          `json:"customer_user_id,omitempty"`
	BuyerNurseryID          *int64          `json:"buyer_nursery_id,omitempty"`
	RecipientName           *string         `json:"recipient_name,omitempty"`
	RecipientMobile         *string         `json:"recipient_mobile,omitempty"`
	// Conversion
	ConvertedOrderID        *int64          `json:"converted_order_id,omitempty"`
	Notes                   *string         `json:"notes,omitempty"`
	TotalAmount             float64         `json:"total_amount"`
	Status                  string          `json:"status"`
	DeletedAt               *time.Time      `json:"deleted_at,omitempty"`
	CreatedAt               time.Time       `json:"created_at"`
	UpdatedAt               time.Time       `json:"updated_at"`
	Items                   []QuotationItem `json:"items,omitempty"`
}

type QuotationItem struct {
	ID                int64     `json:"id"`
	QuotationID       int64     `json:"quotation_id"`
	PlantID           *int64    `json:"plant_id,omitempty"`
	PlantNameSnapshot *string   `json:"plant_name_snapshot,omitempty"`
	ScientificName    string    `json:"scientific_name"`
	CommonName        *string   `json:"common_name,omitempty"`
	Description       *string   `json:"description,omitempty"`
	Size              *string   `json:"size,omitempty"`
	Remarks           *string   `json:"remarks,omitempty"`
	Quantity          float64   `json:"quantity"`
	UnitPrice         float64   `json:"unit_price"`
	TotalPrice        float64   `json:"total_price"`
	CreatedAt         time.Time `json:"created_at"`
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}
