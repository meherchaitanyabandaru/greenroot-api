package drivers

import "time"

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
	actionDelete = "DELETE"
)

type Driver struct {
	ID                int64      `json:"id"`
	DriverCode        string     `json:"driver_code"`
	UserID            *int64     `json:"user_id,omitempty"`
	DriverName        *string    `json:"driver_name,omitempty"`
	Mobile            *string    `json:"mobile,omitempty"`
	LicenseNumber     *string    `json:"license_number,omitempty"`
	LicenseExpiryDate *time.Time `json:"license_expiry_date,omitempty"`
	EmergencyContact  *string    `json:"emergency_contact,omitempty"`
	LicencePhotoURL   *string    `json:"licence_photo_url,omitempty"`
	VehicleNumber     *string    `json:"vehicle_number,omitempty"`
	VehicleType       *string    `json:"vehicle_type,omitempty"`
	ProfileStatus     string     `json:"profile_status"`
	ApprovalStatus    string     `json:"approval_status"`
	ApprovedByUserID  *int64     `json:"approved_by_user_id,omitempty"`
	ApprovedAt        *time.Time `json:"approved_at,omitempty"`
	Status            string     `json:"status"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
}

type DriverLocation struct {
	ID         int64     `json:"id"`
	DriverID   int64     `json:"driver_id"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	RecordedAt time.Time `json:"recorded_at"`
	CreatedBy  *int64    `json:"created_by,omitempty"`
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}
