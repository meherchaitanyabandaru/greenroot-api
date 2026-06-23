package vehicles

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

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

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
