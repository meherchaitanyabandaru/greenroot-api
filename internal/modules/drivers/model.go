package drivers

import (
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"time"
)

type ActorContext = authctx.ActorContext

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
	actionDelete = "DELETE"
)

type Driver struct {
	ID                int64               `json:"id"`
	DriverCode        string              `json:"driver_code"`
	UserID            *int64              `json:"user_id,omitempty"`
	DriverName        *string             `json:"driver_name,omitempty"`
	Mobile            *string             `json:"mobile,omitempty"`
	LicenseNumber     *string             `json:"license_number,omitempty"`
	LicenseExpiryDate *time.Time          `json:"license_expiry_date,omitempty"`
	EmergencyContact  *string             `json:"emergency_contact,omitempty"`
	LicencePhotoURL   *string             `json:"licence_photo_url,omitempty"`
	VehicleNumber     *string             `json:"vehicle_number,omitempty"`
	VehicleType       *string             `json:"vehicle_type,omitempty"`
	ProfileStatus     string              `json:"profile_status"`
	ApprovalStatus    string              `json:"approval_status"`
	ApprovedByUserID  *int64              `json:"approved_by_user_id,omitempty"`
	ApprovedAt        *time.Time          `json:"approved_at,omitempty"`
	Status            string              `json:"status"`
	CreatedAt         time.Time           `json:"created_at"`
	UpdatedAt         *time.Time          `json:"updated_at,omitempty"`
	Capabilities      *DriverCapabilities `json:"capabilities,omitempty"`
	Summary           *DriverSummary      `json:"summary,omitempty"`
}

type DriverCapabilities struct {
	CanEdit           bool `json:"can_edit"`
	CanDelete         bool `json:"can_delete"`
	CanApprove        bool `json:"can_approve"`
	CanUpdateLocation bool `json:"can_update_location"`
}

type DriverSummary struct {
	IsApproved        bool `json:"is_approved"`
	IsProfileComplete bool `json:"is_profile_complete"`
	IsActive          bool `json:"is_active"`
	IsSuspended       bool `json:"is_suspended"`
}

type DriverLocation struct {
	ID         int64     `json:"id"`
	DriverID   int64     `json:"driver_id"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	RecordedAt time.Time `json:"recorded_at"`
	CreatedBy  *int64    `json:"created_by,omitempty"`
}
