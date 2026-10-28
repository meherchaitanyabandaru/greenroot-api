package dispatches

import (
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
)

type ActorContext = authctx.ActorContext

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
)

type Dispatch struct {
	ID                 int64      `json:"id"`
	DispatchCode       string     `json:"dispatch_code"`
	TripUUID           *string    `json:"trip_uuid,omitempty"`
	OrderID            int64      `json:"order_id"`
	OrderNumber        *string    `json:"order_number,omitempty"`
	OrderStatus        *string    `json:"order_status,omitempty"`
	LoadingStartedAt   *time.Time `json:"loading_started_at,omitempty"`
	LoadingCompletedAt *time.Time `json:"loading_completed_at,omitempty"`
	SellerNurseryID    *int64     `json:"seller_nursery_id,omitempty"`
	SellerNurseryName  *string    `json:"seller_nursery_name,omitempty"` // pickup point for the driver
	// V1 snapshot fields
	NurseryID              *int64     `json:"nursery_id,omitempty"`
	AssignedManagerUserID  *int64     `json:"assigned_manager_user_id,omitempty"`
	DriverUserID           *int64     `json:"driver_user_id,omitempty"`
	OwnerUserIDSnapshot    *int64     `json:"owner_user_id_snapshot,omitempty"`
	CustomerUserID         *int64     `json:"customer_user_id,omitempty"`
	CustomerNameSnapshot   *string    `json:"customer_name_snapshot,omitempty"`
	CustomerMobileSnapshot *string    `json:"customer_mobile_snapshot,omitempty"`
	DriverAcceptedAt       *time.Time `json:"driver_accepted_at,omitempty"`
	TripStartedAt          *time.Time `json:"trip_started_at,omitempty"`
	TripStartedByUserID    *int64     `json:"trip_started_by_user_id,omitempty"`
	CompletedAt            *time.Time `json:"completed_at,omitempty"`
	TrackingUUID           *string    `json:"tracking_uuid,omitempty"`
	// Legacy fields
	DispatchNumber     *string    `json:"dispatch_number,omitempty"`
	Status             string     `json:"dispatch_status"`
	VehicleID          *int64     `json:"vehicle_id,omitempty"`
	VehicleNumber      *string    `json:"vehicle_number,omitempty"`
	DriverID           *int64     `json:"driver_id,omitempty"`
	DriverName         *string    `json:"driver_name,omitempty"`
	DriverMobile       *string    `json:"driver_mobile,omitempty"`
	DispatchedBy       *int64     `json:"dispatched_by,omitempty"`
	DispatchDate       *time.Time `json:"dispatch_date,omitempty"`
	DeliveryDate       *time.Time `json:"delivery_date,omitempty"`
	DestinationAddress *string    `json:"destination_address,omitempty"`
	// Delivery coordinates from the order's delivery snapshot.
	// Always reflects the latest confirmed delivery location — drivers use
	// these to open navigation rather than relying on the address text.
	DeliveryLatitude  *float64   `json:"delivery_latitude,omitempty"`
	DeliveryLongitude *float64   `json:"delivery_longitude,omitempty"`
	RequiresDriverAck *bool      `json:"requires_driver_ack,omitempty"`
	Notes             *string    `json:"notes,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
	// Outside lorry: the driver has no app (no QR join, no GPS).
	IsOutsideTransport   bool    `json:"is_outside_transport"`
	OutsideVehicleNumber *string `json:"outside_vehicle_number,omitempty"`
	OutsideDriverName    *string `json:"outside_driver_name,omitempty"`
	OutsideDriverMobile  *string `json:"outside_driver_mobile,omitempty"`
	// The trip QR is the driver's invitation: any approved driver with the
	// lorry asked for (any lorry when empty) may take it until it expires.
	RequiredVehicleType *string    `json:"required_vehicle_type,omitempty"`
	InviteExpiresAt     *time.Time `json:"invite_expires_at,omitempty"`
	// How the customer confirmed delivery: CUSTOMER_CODE, CUSTOMER_APP or
	// UNCONFIRMED (nursery override, with a reason).
	DeliveryConfirmation      *string    `json:"delivery_confirmation,omitempty"`
	DeliveryConfirmedByUserID *int64     `json:"delivery_confirmed_by_user_id,omitempty"`
	DeliveryConfirmedAt       *time.Time `json:"delivery_confirmed_at,omitempty"`
	DeliveryOverrideReason    *string    `json:"delivery_override_reason,omitempty"`
	DeliveryIssue             *string    `json:"delivery_issue,omitempty"`
	DeliveryIssueReportedAt   *time.Time `json:"delivery_issue_reported_at,omitempty"`
	DeliveryCodeAttempts      int        `json:"-"`
	// Only in the customer's own view, while the trip is on its way.
	DeliveryCode *string `json:"delivery_code,omitempty"`
	// Version changes on every update; send it back as If-Match.
	Version      int64                       `json:"version"`
	Items        []DispatchItem              `json:"items,omitempty"`
	TripEvents   []TripEvent                 `json:"trip_events,omitempty"`
	Lifecycle    *lifecycle.DispatchDisplays `json:"lifecycle,omitempty"`
	Capabilities *DispatchCapabilities       `json:"capabilities,omitempty"`
}

type TripEvent struct {
	ID              int64     `json:"id"`
	DispatchID      int64     `json:"dispatch_id"`
	EventType       string    `json:"event_type"`
	Latitude        *float64  `json:"latitude,omitempty"`
	Longitude       *float64  `json:"longitude,omitempty"`
	PhotoURL        *string   `json:"photo_url,omitempty"`
	Remarks         *string   `json:"remarks,omitempty"`
	CreatedByUserID int64     `json:"created_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
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
	OrderID     int64
	BuyerID     *int64
	NurseryID   *int64
	OrderStatus string
}

type PublicTrackingInfo struct {
	Dispatch *Dispatch `json:"dispatch"`
	Status   string    `json:"status"`
}
