package nurseries

import (
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
)

type Nursery struct {
	ID              int64      `json:"id"`
	Code            *string    `json:"code,omitempty"`
	NurseryCode     *string    `json:"nursery_code,omitempty"`
	Name            string     `json:"name"`
	Mobile          *string    `json:"mobile,omitempty"`
	Email           *string    `json:"email,omitempty"`
	Website         *string    `json:"website,omitempty"`
	Description     *string    `json:"description,omitempty"`
	Status          string     `json:"status"`
	RejectionReason *string    `json:"rejection_reason,omitempty"`
	RejectedAt      *time.Time `json:"rejected_at,omitempty"`
	OwnerUserID     *int64     `json:"owner_user_id,omitempty"`
	// Branding — set after nursery approval via PUT /nurseries/:id
	LogoURL      *string                    `json:"logo_url,omitempty"`
	BrandIconKey *string                    `json:"brand_icon_key,omitempty"`
	BrandColor   *string                    `json:"brand_color,omitempty"`
	CreatedAt    time.Time                  `json:"created_at"`
	UpdatedAt    time.Time                  `json:"updated_at"`
	CreatedBy    *int64                     `json:"created_by,omitempty"`
	UpdatedBy    *int64                     `json:"updated_by,omitempty"`
	Addresses    []Address                  `json:"addresses,omitempty"`
	Users        []UserLink                 `json:"users,omitempty"`
	Lifecycle    *lifecycle.NurseryDisplays `json:"lifecycle,omitempty"`
	Capabilities *NurseryCapabilities       `json:"capabilities,omitempty"`
	Summary      *NurserySummary            `json:"summary,omitempty"`
}

type NurseryCapabilities struct {
	CanEdit            bool `json:"can_edit"`
	CanDelete          bool `json:"can_delete"`
	CanApprove         bool `json:"can_approve"`
	CanReject          bool `json:"can_reject"`
	CanSuspend         bool `json:"can_suspend"`
	CanReactivate      bool `json:"can_reactivate"`
	CanManageInventory bool `json:"can_manage_inventory"`
	CanManageUsers     bool `json:"can_manage_users"`
	CanManageAddresses bool `json:"can_manage_addresses"`
}

type NurserySummary struct {
	IsOwner     bool `json:"is_owner"`
	IsApproved  bool `json:"is_approved"`
	IsPending   bool `json:"is_pending"`
	IsSuspended bool `json:"is_suspended"`
	IsDeleted   bool `json:"is_deleted"`
}

// NurseryDriver represents a driver connected to a nursery.
type NurseryDriver struct {
	ID               int64      `json:"id"`
	NurseryID        int64      `json:"nursery_id"`
	DriverUserID     int64      `json:"driver_user_id"`
	DriverName       *string    `json:"driver_name,omitempty"`
	DriverMobile     *string    `json:"driver_mobile,omitempty"`
	VehicleNumber    *string    `json:"vehicle_number,omitempty"`
	VehicleType      *string    `json:"vehicle_type,omitempty"`
	ConnectionStatus string     `json:"connection_status"`
	ConnectedAt      *time.Time `json:"connected_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type Address struct {
	ID                  int64      `json:"id"`
	NurseryID           int64      `json:"nursery_id"`
	AddressType         *string    `json:"address_type,omitempty"`
	AddressLine1        *string    `json:"address_line1,omitempty"`
	AddressLine2        *string    `json:"address_line2,omitempty"`
	City                *string    `json:"city,omitempty"`
	State               *string    `json:"state,omitempty"`
	Country             *string    `json:"country,omitempty"`
	PostalCode          *string    `json:"postal_code,omitempty"`
	Latitude            *float64   `json:"latitude,omitempty"`
	Longitude           *float64   `json:"longitude,omitempty"`
	GPSAccuracyM        *float64   `json:"gps_accuracy_meters,omitempty"`
	Landmark            *string    `json:"landmark,omitempty"`
	LocationSource      *string    `json:"location_source,omitempty"`
	LocationConfirmedBy *int64     `json:"location_confirmed_by,omitempty"`
	LocationConfirmedAt *time.Time `json:"location_confirmed_at,omitempty"`
	IsPrimary           bool       `json:"is_primary"`
	CreatedAt           *time.Time `json:"created_at,omitempty"`
	UpdatedAt           *time.Time `json:"updated_at,omitempty"`
}

type UserLink struct {
	ID        int64      `json:"id"`
	NurseryID int64      `json:"nursery_id"`
	UserID    int64      `json:"user_id"`
	FirstName string     `json:"first_name"`
	LastName  *string    `json:"last_name,omitempty"`
	FullName  string     `json:"full_name"`
	Mobile    string     `json:"mobile"`
	Email     *string    `json:"email,omitempty"`
	RoleID    int16      `json:"role_id,omitempty"`
	RoleCode  string     `json:"role_code"`
	RoleName  string     `json:"role_name"`
	Role      string     `json:"role"` // V1 text role: MANAGER | GUMASTHA
	Status    string     `json:"status"`
	JoinedAt  *time.Time `json:"joined_at,omitempty"`
	IsActive  bool       `json:"is_active"`
}

// Customer represents a buyer who accepted a CUSTOMER_INVITE for this nursery.
type Customer struct {
	UserID     int64      `json:"user_id"`
	FirstName  string     `json:"first_name"`
	LastName   *string    `json:"last_name,omitempty"`
	FullName   string     `json:"full_name"`
	Mobile     string     `json:"mobile"`
	Email      *string    `json:"email,omitempty"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}
