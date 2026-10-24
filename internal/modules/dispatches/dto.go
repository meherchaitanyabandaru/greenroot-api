package dispatches

import (
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
)

type ListDispatchesRequest struct {
	Page           int
	PerPage        int
	OrderID        int64
	NurseryID      int64
	DriverUserID   int64
	BuyerUserID    int64
	BuyerNurseryID int64
	Buying         bool
	Status         string
	Search         string
	SortBy         string
	SortOrder      string
}

type CreateDispatchRequest struct {
	OrderID            int64                 `json:"order_id"`
	DispatchNumber     *string               `json:"dispatch_number"`
	VehicleID          *int64                `json:"vehicle_id"`
	DriverID           *int64                `json:"driver_id"`
	DispatchDate       *string               `json:"dispatch_date"`
	DestinationAddress *string               `json:"destination_address"`
	Notes              *string               `json:"notes"`
	Items              []DispatchItemRequest `json:"items"`
	// Outside lorry whose driver has no app: the vehicle number is required.
	IsOutsideTransport   bool    `json:"is_outside_transport"`
	OutsideVehicleNumber *string `json:"outside_vehicle_number"`
	OutsideDriverName    *string `json:"outside_driver_name"`
	OutsideDriverMobile  *string `json:"outside_driver_mobile"`
}

// DeliverRequest marks a trip delivered: with the customer's delivery code,
// or (nursery staff only) without it, giving the reason.
type DeliverRequest struct {
	Code           string  `json:"code"`
	OverrideReason *string `json:"override_reason"`
}

// ReceiptRequest is the customer's side: confirm they received the plants
// and/or report a problem.
type ReceiptRequest struct {
	Issue *string `json:"issue"`
}

type UpdateStatusRequest struct {
	Status       string  `json:"dispatch_status"`
	StatusAlias  string  `json:"status"`
	DeliveryDate *string `json:"delivery_date"`
	Notes        *string `json:"notes"`
}

type DispatchItemRequest struct {
	OrderItemID *int64  `json:"order_item_id"`
	Quantity    float64 `json:"quantity"`
	Notes       *string `json:"notes"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type DispatchesResponse struct {
	Dispatches []Dispatch `json:"dispatches"`
	Pagination Pagination `json:"pagination"`
}

type DispatchResponse struct {
	Dispatch Dispatch `json:"dispatch"`
}

type DispatchItemResponse struct {
	Item DispatchItem `json:"item"`
}

type CreateTripEventRequest struct {
	EventType string   `json:"event_type"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	PhotoURL  *string  `json:"photo_url"`
	Remarks   *string  `json:"remarks"`
}

type TripEventResponse struct {
	Event TripEvent `json:"event"`
}

type PublicTrackingResponse struct {
	Dispatch PublicTrip `json:"dispatch"`
}

// PublicTrip is everything an unauthenticated tracking link shows: the trip's
// progress and the lorry's position while it is on the way. It never carries
// the customer's address or delivery coordinates, phone numbers, names of
// people, notes or internal ids (anyone holding the link sees it, forever).
type PublicTrip struct {
	DispatchCode      string                      `json:"dispatch_code"`
	Status            string                      `json:"dispatch_status"`
	Lifecycle         *lifecycle.DispatchDisplays `json:"lifecycle,omitempty"`
	SellerNurseryName *string                     `json:"seller_nursery_name,omitempty"`
	VehicleNumber     *string                     `json:"vehicle_number,omitempty"`
	DispatchDate      *time.Time                  `json:"dispatch_date,omitempty"`
	TripStartedAt     *time.Time                  `json:"trip_started_at,omitempty"`
	DeliveryDate      *time.Time                  `json:"delivery_date,omitempty"`
	// Positions only while the trip is on its way; after delivery the link
	// shows the outcome without the route that led to the customer.
	TripEvents []PublicTripEvent `json:"trip_events,omitempty"`
}

type PublicTripEvent struct {
	EventType string    `json:"event_type"`
	Latitude  *float64  `json:"latitude,omitempty"`
	Longitude *float64  `json:"longitude,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
