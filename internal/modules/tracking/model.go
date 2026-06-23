package tracking

import "time"

type TrackingPoint struct {
	ID         int64     `json:"id"`
	VehicleID  *int64    `json:"vehicle_id,omitempty"`
	DriverID   *int64    `json:"driver_id,omitempty"`
	DispatchID *int64    `json:"dispatch_id,omitempty"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	TrackedAt  time.Time `json:"tracked_at"`
	Notes      *string   `json:"notes,omitempty"`
}
type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}
