package requests

import "time"

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
	actionDelete = "DELETE"
)

type PlantRequest struct {
	ID                  int64      `json:"id"`
	RequestCode         string     `json:"request_code"`
	RequestingNurseryID int64      `json:"requesting_nursery_id"`
	RequestingNursery   string     `json:"requesting_nursery"`
	RequestedByUserID   int64      `json:"requested_by_user_id"`
	RequestedByName     string     `json:"requested_by_name"`
	PlantID             int64      `json:"plant_id"`
	ScientificName      string     `json:"scientific_name"`
	CommonName          *string    `json:"common_name,omitempty"`
	SizeID              *int16     `json:"size_id,omitempty"`
	SizeCode            *string    `json:"size_code,omitempty"`
	SizeName            *string    `json:"size_name,omitempty"`
	QuantityRequired    int        `json:"quantity_required"`
	RadiusKM            int        `json:"radius_km"`
	Notes               *string    `json:"notes,omitempty"`
	Status              string     `json:"status"`
	ExpiresAt           *time.Time `json:"expires_at,omitempty"`
	FulfilledAt         *time.Time `json:"fulfilled_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	Responses           []Response `json:"responses,omitempty"`
}

type Response struct {
	ID                int64     `json:"id"`
	RequestID         int64     `json:"request_id"`
	SupplierNurseryID int64     `json:"supplier_nursery_id"`
	SupplierNursery   string    `json:"supplier_nursery"`
	RespondedByUserID int64     `json:"responded_by_user_id"`
	RespondedByName   string    `json:"responded_by_name"`
	AvailableQuantity int       `json:"available_quantity"`
	Remarks           *string   `json:"remarks,omitempty"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}
