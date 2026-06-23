package inventory

type ListInventoryRequest struct {
	Page      int
	PerPage   int
	Search    string
	NurseryID int64
	PlantID   int64
	SizeID    int16
	Status    string
	SortBy    string
	SortOrder string
}

type UpsertInventoryRequest struct {
	NurseryID         int64  `json:"nursery_id"`
	PlantID           int64  `json:"plant_id"`
	SizeID            int16  `json:"size_id"`
	AvailableQuantity int    `json:"available_quantity"`
	Status            string `json:"inventory_status"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type InventoryListResponse struct {
	Inventory  []InventoryItem `json:"inventory"`
	Pagination Pagination      `json:"pagination"`
}

type InventoryResponse struct {
	Inventory InventoryItem `json:"inventory"`
}

type MessageResponse struct {
	Message string `json:"message"`
}
