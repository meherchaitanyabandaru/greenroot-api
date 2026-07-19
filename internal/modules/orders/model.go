package orders

import (
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
)

type ActorContext = authctx.ActorContext

type Order struct {
	ID          int64  `json:"id"`
	OrderCode   string `json:"order_code"`
	OrderNumber string `json:"order_number"`
	// V1 nursery-centric fields
	NurseryID                *int64     `json:"nursery_id,omitempty"`
	NurseryName              *string    `json:"nursery_name,omitempty"`
	QuotationID              *int64     `json:"quotation_id,omitempty"`
	CustomerUserID           *int64     `json:"customer_user_id,omitempty"`
	CustomerName             *string    `json:"customer_name,omitempty"`
	CustomerMobile           *string    `json:"customer_mobile,omitempty"`
	AssignedManagerUserID    *int64     `json:"assigned_manager_user_id,omitempty"`
	AssignedManagerName      *string    `json:"assigned_manager_name,omitempty"`
	CreatedByUserID          *int64     `json:"created_by_user_id,omitempty"`
	CancelledByUserID        *int64     `json:"cancelled_by_user_id,omitempty"`
	CancelledAt              *time.Time `json:"cancelled_at,omitempty"`
	CancelReason             *string    `json:"cancel_reason,omitempty"`
	LoadingStartedAt         *time.Time `json:"loading_started_at,omitempty"`
	LoadingCompletedAt       *time.Time `json:"loading_completed_at,omitempty"`
	LoadingCompletedByUserID *int64     `json:"loading_completed_by_user_id,omitempty"`
	// Buyer fields
	BuyerUserID    *int64  `json:"buyer_user_id,omitempty"`
	BuyerNurseryID *int64  `json:"buyer_nursery_id,omitempty"`
	BuyerName      *string `json:"buyer_name,omitempty"`
	// Seller fields
	SellerNurseryID      *int64                   `json:"seller_nursery_id,omitempty"`
	SellerNursery        *string                  `json:"seller_nursery,omitempty"`
	Status               string                   `json:"order_status"`
	TotalAmount          float64                  `json:"total_amount"`
	Notes                *string                  `json:"notes,omitempty"`
	OrderDate            time.Time                `json:"order_date"`
	CreatedAt            time.Time                `json:"created_at"`
	UpdatedAt            time.Time                `json:"updated_at"`
	Items                []OrderItem              `json:"items,omitempty"`
	DeliverySnapshot     *DeliverySnapshot        `json:"delivery_snapshot,omitempty"`
	ActiveDispatch       *ActiveDispatchSummary   `json:"active_dispatch,omitempty"`
	ActiveDispatchID     *int64                   `json:"active_dispatch_id,omitempty"`
	ActiveDispatchStatus *string                  `json:"active_dispatch_status,omitempty"`
	Lifecycle            *lifecycle.OrderDisplays `json:"lifecycle,omitempty"`
	Capabilities         *OrderCapabilities       `json:"capabilities,omitempty"`
}

type ActiveDispatchSummary struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

type DeliverySnapshot struct {
	ID                   int64      `json:"id"`
	OrderID              int64      `json:"order_id"`
	ContactName          *string    `json:"contact_name,omitempty"`
	ContactMobile        *string    `json:"contact_mobile,omitempty"`
	AlternateMobile      *string    `json:"alternate_mobile,omitempty"`
	AddressLine1         *string    `json:"address_line1,omitempty"`
	AddressLine2         *string    `json:"address_line2,omitempty"`
	City                 *string    `json:"city,omitempty"`
	State                *string    `json:"state,omitempty"`
	Country              *string    `json:"country,omitempty"`
	PostalCode           *string    `json:"postal_code,omitempty"`
	Landmark             *string    `json:"landmark,omitempty"`
	DeliveryInstructions *string    `json:"delivery_instructions,omitempty"`
	Latitude             *float64   `json:"latitude,omitempty"`
	Longitude            *float64   `json:"longitude,omitempty"`
	GPSAccuracyM         *float64   `json:"gps_accuracy_meters,omitempty"`
	LocationSource       *string    `json:"location_source,omitempty"`
	ConfirmedBy          *int64     `json:"confirmed_by,omitempty"`
	ConfirmedAt          *time.Time `json:"confirmed_at,omitempty"`
	EmergencyUpdated     bool       `json:"emergency_updated"`
	RequiresDriverAck    bool       `json:"requires_driver_ack"`
	DriverAcknowledgedBy *int64     `json:"driver_acknowledged_by,omitempty"`
	DriverAcknowledgedAt *time.Time `json:"driver_acknowledged_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
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
	LoadedQuantity *float64  `json:"loaded_quantity,omitempty"`
	UnitPrice      float64   `json:"unit_price"`
	TotalPrice     float64   `json:"total_price"`
	Remarks        *string   `json:"remarks,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}
