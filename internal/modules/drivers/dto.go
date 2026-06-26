package drivers

type ListDriversRequest struct {
	Page      int
	PerPage   int
	Status    string
	Search    string
	SortBy    string
	SortOrder string
}

type DriverRequest struct {
	UserID            *int64  `json:"user_id"`
	LicenseNumber     *string `json:"license_number"`
	LicenseExpiryDate *string `json:"license_expiry_date"`
	EmergencyContact  *string `json:"emergency_contact"`
	Status            string  `json:"status"`
}

// ApplyDriverRequest is used by a user to self-register as a driver (V1 flow).
type ApplyDriverRequest struct {
	LicenceNumber   string  `json:"licence_number"`
	LicencePhotoURL *string `json:"licence_photo_url"`
	VehicleNumber   string  `json:"vehicle_number"`
	VehicleType     string  `json:"vehicle_type"`
}

// ApproveDriverRequest is used by admin to approve a driver profile.
type ApproveDriverRequest struct {
	DriverUserID int64 `json:"driver_user_id"`
}

type LocationRequest struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type DriversResponse struct {
	Drivers    []Driver   `json:"drivers"`
	Pagination Pagination `json:"pagination"`
}

type DriverResponse struct {
	Driver Driver `json:"driver"`
}

type LocationResponse struct {
	Location DriverLocation `json:"location"`
}

type MessageResponse struct {
	Message string `json:"message"`
}
