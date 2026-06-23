package orders

import "time"

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
	actionDelete = "DELETE"
)

type Order struct {
	ID              int64       `json:"id"`
	OrderCode       string      `json:"order_code"`
	OrderNumber     string      `json:"order_number"`
	BuyerUserID     *int64      `json:"buyer_user_id,omitempty"`
	BuyerName       *string     `json:"buyer_name,omitempty"`
	SellerNurseryID *int64      `json:"seller_nursery_id,omitempty"`
	SellerNursery   *string     `json:"seller_nursery,omitempty"`
	Status          string      `json:"order_status"`
	TotalAmount     float64     `json:"total_amount"`
	Notes           *string     `json:"notes,omitempty"`
	OrderDate       time.Time   `json:"order_date"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
	CreatedBy       *int64      `json:"created_by,omitempty"`
	UpdatedBy       *int64      `json:"updated_by,omitempty"`
	Items           []OrderItem `json:"items,omitempty"`
}

type OrderItem struct {
	ID             int64     `json:"id"`
	OrderID        int64     `json:"order_id"`
	PlantID        int64     `json:"plant_id"`
	ScientificName string    `json:"scientific_name"`
	CommonName     *string   `json:"common_name,omitempty"`
	SizeID         *int16    `json:"size_id,omitempty"`
	SizeCode       *string   `json:"size_code,omitempty"`
	SizeName       *string   `json:"size_name,omitempty"`
	Quantity       float64   `json:"quantity"`
	UnitPrice      float64   `json:"unit_price"`
	TotalPrice     float64   `json:"total_price"`
	Remarks        *string   `json:"remarks,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}
