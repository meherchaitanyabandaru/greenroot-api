package requests

import "time"

type ListRequestsRequest struct {
	Page      int
	PerPage   int
	NurseryID int64
	PlantID   int64
	Status    string
	Search    string
}

type CreateRequest struct {
	RequestingNurseryID int64      `json:"requesting_nursery_id"`
	PlantID             int64      `json:"plant_id"`
	SizeID              *int16     `json:"size_id"`
	QuantityRequired    int        `json:"quantity_required"`
	RadiusKM            int        `json:"radius_km"`
	RequiredByDate      *time.Time `json:"required_by_date"`
	Notes               *string    `json:"notes"`
	Status              string     `json:"status"`
	ExpiresAt           *time.Time `json:"expires_at"`
}

type UpdateRequest = CreateRequest

// UpdateStatusRequest is used by PUT /{id}/status — manager advances request lifecycle.
type UpdateStatusRequest struct {
	Status string `json:"status"`
}

// CreateResponseRequest is submitted by the supplier nursery with their availability.
// Status must be AVAILABLE, PARTIAL, or NOT_AVAILABLE.
type CreateResponseRequest struct {
	SupplierNurseryID int64   `json:"supplier_nursery_id"`
	AvailableQuantity int     `json:"available_quantity"`
	Remarks           *string `json:"remarks"`
	Status            string  `json:"status"`
}

// UpdateResponseRequest is used by the requesting manager to select or reject a supplier.
// Status must be ACCEPTED or REJECTED.
type UpdateResponseRequest struct {
	Status string `json:"status"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type RequestsResponse struct {
	Requests   []PlantRequest `json:"requests"`
	Pagination Pagination     `json:"pagination"`
}

type RequestResponse struct {
	Request PlantRequest `json:"request"`
}

type ResponsesResponse struct {
	Responses []Response `json:"responses"`
}

type SingleResponse struct {
	Response Response `json:"response"`
}

type MessageResponse struct {
	Message string `json:"message"`
}
