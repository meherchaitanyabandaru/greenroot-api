package dispatches

import "time"

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
)

type Dispatch struct {
	ID                 int64          `json:"id"`
	DispatchCode       string         `json:"dispatch_code"`
	OrderID            int64          `json:"order_id"`
	OrderNumber        *string        `json:"order_number,omitempty"`
	SellerNurseryID    *int64         `json:"seller_nursery_id,omitempty"`
	DispatchNumber     *string        `json:"dispatch_number,omitempty"`
	Status             string         `json:"dispatch_status"`
	VehicleID          *int64         `json:"vehicle_id,omitempty"`
	VehicleNumber      *string        `json:"vehicle_number,omitempty"`
	DriverID           *int64         `json:"driver_id,omitempty"`
	DriverName         *string        `json:"driver_name,omitempty"`
	DispatchedBy       *int64         `json:"dispatched_by,omitempty"`
	DispatchDate       *time.Time     `json:"dispatch_date,omitempty"`
	DeliveryDate       *time.Time     `json:"delivery_date,omitempty"`
	DestinationAddress *string        `json:"destination_address,omitempty"`
	Notes              *string        `json:"notes,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          *time.Time     `json:"updated_at,omitempty"`
	Items              []DispatchItem `json:"items,omitempty"`
}

type DispatchItem struct {
	ID          int64     `json:"id"`
	DispatchID  int64     `json:"dispatch_id"`
	OrderItemID *int64    `json:"order_item_id,omitempty"`
	PlantID     *int64    `json:"plant_id,omitempty"`
	PlantName   *string   `json:"plant_name,omitempty"`
	Quantity    float64   `json:"quantity"`
	Notes       *string   `json:"notes,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type OrderAccess struct {
	OrderID   int64
	BuyerID   *int64
	NurseryID *int64
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}
