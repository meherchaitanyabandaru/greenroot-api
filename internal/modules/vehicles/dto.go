package vehicles

import "github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"

type ListVehiclesRequest struct {
	Page      int
	PerPage   int
	Status    string
	Type      string
	Search    string
	OwnerID   int64
	Approval  string // approval_status filter (comma list)
	SortBy    string
	SortOrder string
}

type VehicleRequest struct {
	VehicleNumber string   `json:"vehicle_number"`
	VehicleType   *string  `json:"vehicle_type"`
	CapacityKG    *float64 `json:"capacity_kg"`
	// OwnerID is a vehicle_owners id (GET /vehicle-owners); owner_name and
	// mobile are no longer written here.
	OwnerID *int64 `json:"owner_id"`
	Status  string `json:"status"`
}

type Pagination = response.Pagination

type VehiclesResponse struct {
	Vehicles   []Vehicle  `json:"vehicles"`
	Pagination Pagination `json:"pagination"`
}

type VehicleResponse struct {
	Vehicle Vehicle `json:"vehicle"`
}

type OwnersResponse struct {
	Owners     []VehicleOwner `json:"owners"`
	Pagination Pagination     `json:"pagination"`
}

type OwnerResponse struct {
	Owner VehicleOwner `json:"owner"`
}

type LinksResponse struct {
	Links      []VehicleDriverLink `json:"links"`
	Pagination Pagination          `json:"pagination"`
}

type LinkResponse struct {
	Link VehicleDriverLink `json:"link"`
}

type MessageResponse struct {
	Message string `json:"message"`
}
