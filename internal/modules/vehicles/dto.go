package vehicles

import "github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"

type ListVehiclesRequest struct {
	Page      int
	PerPage   int
	Status    string
	Type      string
	Search    string
	SortBy    string
	SortOrder string
}

type VehicleRequest struct {
	VehicleNumber string   `json:"vehicle_number"`
	VehicleType   *string  `json:"vehicle_type"`
	CapacityKG    *float64 `json:"capacity_kg"`
	OwnerName     *string  `json:"owner_name"`
	Mobile        *string  `json:"mobile"`
	Status        string   `json:"status"`
}

type Pagination = response.Pagination

type VehiclesResponse struct {
	Vehicles   []Vehicle  `json:"vehicles"`
	Pagination Pagination `json:"pagination"`
}

type VehicleResponse struct {
	Vehicle Vehicle `json:"vehicle"`
}

type MessageResponse struct {
	Message string `json:"message"`
}
