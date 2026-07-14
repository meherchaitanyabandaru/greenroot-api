package tracking

type CreateRequest struct {
	VehicleID  *int64  `json:"vehicle_id"`
	DriverID   *int64  `json:"driver_id"`
	DispatchID *int64  `json:"dispatch_id"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	Notes      *string `json:"notes"`
}
type ListResponse struct {
	Tracking []TrackingPoint `json:"tracking"`
}
type PointResponse struct {
	Tracking *TrackingPoint `json:"tracking"`
}

type LiveLocationRequest struct {
	DriverUserID *int64  `json:"driver_user_id,omitempty"`
	DispatchID   *int64  `json:"dispatch_id,omitempty"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
}

type LiveLocationResponse struct {
	Location *LiveDriverLocation `json:"location"`
}

type NearbyLiveDriversResponse struct {
	Drivers []NearbyLiveDriver `json:"drivers"`
}

type LiveDriverLocation struct {
	DriverUserID int64   `json:"driver_user_id"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	LastSeen     string  `json:"last_seen"`
}

type NearbyLiveDriver struct {
	LiveDriverLocation
	DistanceKM float64 `json:"distance_km"`
}
