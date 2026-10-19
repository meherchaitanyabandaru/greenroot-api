package topitems

type ListRequest struct {
	Page      int
	PerPage   int
	Search    string
	NurseryID int64
	PlantID   int64
	SizeID    int16
	SortBy    string
	SortOrder string
}

// CreateRequest adds a plant + size to a nursery's top items.
type CreateRequest struct {
	NurseryID    int64    `json:"nursery_id"`
	PlantID      int64    `json:"plant_id"`
	SizeID       int16    `json:"size_id"`
	UnitPrice    *float64 `json:"unit_price"`
	DisplayOrder *int16   `json:"display_order"`
	QualityNotes *string  `json:"quality_notes"`
	Photos       []string `json:"photos"`
}

// UpdateRequest edits an item in place; it never moves to another nursery.
type UpdateRequest struct {
	PlantID      int64    `json:"plant_id"`
	SizeID       int16    `json:"size_id"`
	UnitPrice    *float64 `json:"unit_price"`
	DisplayOrder *int16   `json:"display_order"`
	QualityNotes *string  `json:"quality_notes"`
	Photos       []string `json:"photos"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type ListResponse struct {
	TopItems   []TopItem  `json:"top_items"`
	Pagination Pagination `json:"pagination"`
}

type ItemResponse struct {
	TopItem TopItem `json:"top_item"`
}

type MessageResponse struct {
	Message string `json:"message"`
}
