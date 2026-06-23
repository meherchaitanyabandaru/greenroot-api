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
	Tracking TrackingPoint `json:"tracking"`
}
