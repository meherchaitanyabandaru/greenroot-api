package vehicles

import "time"


type Vehicle struct {
	ID            int64      `json:"id"`
	VehicleCode   string     `json:"vehicle_code"`
	VehicleNumber string     `json:"vehicle_number"`
	VehicleType   *string    `json:"vehicle_type,omitempty"`
	CapacityKG    *float64   `json:"capacity_kg,omitempty"`
	OwnerName     *string    `json:"owner_name,omitempty"`
	Mobile        *string    `json:"mobile,omitempty"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}
