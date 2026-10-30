package drivers

import (
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

type ListDriversRequest struct {
	Page    int
	PerPage int
	Status  string
	// ApprovalStatus filters PENDING / APPROVED / REJECTED (comma list) so
	// admins can find applications waiting for review.
	ApprovalStatus string
	Search         string
	SortBy         string
	SortOrder      string
}

type DriverRequest struct {
	UserID            *int64  `json:"user_id"`
	LicenseNumber     *string `json:"license_number"`
	LicenseExpiryDate *string `json:"license_expiry_date"`
	EmergencyContact  *string `json:"emergency_contact"`
	Status            string  `json:"status"`
}

// ApplyDriverRequest is used by a user to self-register as a driver (V1 flow).
// The lorry it names becomes the driver's first lorry link; without is_owner
// or owner_name (older apps) the driver is taken as its owner.
type ApplyDriverRequest struct {
	LicenceNumber   string  `json:"licence_number"`
	LicencePhotoURL *string `json:"licence_photo_url"`
	// LicenceExpiryDate is YYYY-MM-DD; optional for older apps, never in the past.
	LicenceExpiryDate *string `json:"licence_expiry_date"`
	VehicleNumber     string  `json:"vehicle_number"`
	VehicleType       string  `json:"vehicle_type"`
	IsOwner           bool    `json:"is_owner"`
	OwnerName         *string `json:"owner_name"`
	OwnerMobile       *string `json:"owner_mobile"`

	expiry *time.Time // parsed LicenceExpiryDate
	owner  ownerInput // checked owner_name/owner_mobile
}

// ApproveDriverRequest is used by admin to approve a driver profile.
type ApproveDriverRequest struct {
	DriverUserID int64 `json:"driver_user_id"`
}

type LocationRequest struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Pagination = response.Pagination

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
