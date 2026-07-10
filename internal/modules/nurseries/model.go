package nurseries

import "time"


type Nursery struct {
	ID              int64      `json:"id"`
	Code            *string    `json:"code,omitempty"`
	NurseryCode     *string    `json:"nursery_code,omitempty"`
	Name            string     `json:"name"`
	GSTNumber       *string    `json:"gst_number,omitempty"`
	Mobile          *string    `json:"mobile,omitempty"`
	Email           *string    `json:"email,omitempty"`
	Website         *string    `json:"website,omitempty"`
	Description     *string    `json:"description,omitempty"`
	Status          string     `json:"status"`
	RejectionReason *string    `json:"rejection_reason,omitempty"`
	RejectedAt      *time.Time `json:"rejected_at,omitempty"`
	OwnerUserID     *int64     `json:"owner_user_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CreatedBy       *int64     `json:"created_by,omitempty"`
	UpdatedBy       *int64     `json:"updated_by,omitempty"`
	Addresses       []Address  `json:"addresses,omitempty"`
	Users           []UserLink `json:"users,omitempty"`
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
	ID           int64      `json:"id"`
	NurseryID    int64      `json:"nursery_id"`
	AddressType  *string    `json:"address_type,omitempty"`
	AddressLine1 *string    `json:"address_line1,omitempty"`
	AddressLine2 *string    `json:"address_line2,omitempty"`
	City         *string    `json:"city,omitempty"`
	State        *string    `json:"state,omitempty"`
	Country      *string    `json:"country,omitempty"`
	PostalCode   *string    `json:"postal_code,omitempty"`
	Latitude     *float64   `json:"latitude,omitempty"`
	Longitude    *float64   `json:"longitude,omitempty"`
	IsPrimary    bool       `json:"is_primary"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
}

type UserLink struct {
	ID        int64      `json:"id"`
	NurseryID int64      `json:"nursery_id"`
	UserID    int64      `json:"user_id"`
	FirstName string     `json:"first_name"`
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
