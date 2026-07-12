package orders

import "time"

type ListOrdersRequest struct {
	Page      int
	PerPage   int
	Search    string
	BuyerID   int64
	NurseryID int64
	Status    string
	SortBy    string
	SortOrder string
	Buying    bool // true = buyer perspective (buyer_user_id or buyer_nursery_id filter)
}

type CreateOrderRequest struct {
	OrderNumber     *string                  `json:"order_number"`
	BuyerUserID     *int64                   `json:"buyer_user_id"`
	BuyerMobile     *string                  `json:"buyer_mobile"`
	BuyerName       *string                  `json:"buyer_name"`
	BuyerNurseryID  *int64                   `json:"buyer_nursery_id"`
	SellerNurseryID *int64                   `json:"seller_nursery_id"`
	Status          string                   `json:"order_status"`
	Notes           *string                  `json:"notes"`
	Items           []OrderItemRequest       `json:"items"`
	Delivery        *DeliverySnapshotRequest `json:"delivery"`
}

type UpdateStatusRequest struct {
	Status string `json:"order_status"`
}

type DeliverySnapshotRequest struct {
	ContactName          *string    `json:"contact_name"`
	ContactMobile        *string    `json:"contact_mobile"`
	AlternateMobile      *string    `json:"alternate_mobile"`
	AddressLine1         *string    `json:"address_line1"`
	AddressLine2         *string    `json:"address_line2"`
	City                 *string    `json:"city"`
	State                *string    `json:"state"`
	Country              *string    `json:"country"`
	PostalCode           *string    `json:"postal_code"`
	Landmark             *string    `json:"landmark"`
	DeliveryInstructions *string    `json:"delivery_instructions"`
	Latitude             *float64   `json:"latitude"`
	Longitude            *float64   `json:"longitude"`
	GPSAccuracyM         *float64   `json:"gps_accuracy_meters"`
	LocationSource       *string    `json:"location_source"`
	ConfirmedBy          *int64     `json:"confirmed_by,omitempty"`
	ConfirmedAt          *time.Time `json:"confirmed_at,omitempty"`
	EmergencyUpdate      bool       `json:"emergency_update,omitempty"`
}

type OrderItemRequest struct {
	PlantID    int64   `json:"plant_id"`
	SizeID     *int16  `json:"size_id"`
	Quantity   float64 `json:"quantity"`
	UnitPrice  float64 `json:"unit_price"`
	TotalPrice float64 `json:"total_price"`
	Remarks    *string `json:"remarks"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type OrdersResponse struct {
	Orders     []Order    `json:"orders"`
	Pagination Pagination `json:"pagination"`
}

type OrderResponse struct {
	Order Order `json:"order"`
}

type ItemsResponse struct {
	Items []OrderItem `json:"items"`
}

type ItemResponse struct {
	Item OrderItem `json:"item"`
}

type SetLoadedQuantityRequest struct {
	LoadedQuantity float64 `json:"loaded_quantity"`
}

type MessageResponse struct {
	Message string `json:"message"`
}
