package dispatches

type ListDispatchesRequest struct {
	Page         int
	PerPage      int
	OrderID      int64
	NurseryID    int64
	DriverUserID int64
	Status       string
	Search       string
	SortBy       string
	SortOrder    string
}

type CreateDispatchRequest struct {
	OrderID            int64                 `json:"order_id"`
	DispatchNumber     *string               `json:"dispatch_number"`
	VehicleID          *int64                `json:"vehicle_id"`
	DriverID           *int64                `json:"driver_id"`
	DispatchDate       *string               `json:"dispatch_date"`
	DestinationAddress *string               `json:"destination_address"`
	Notes              *string               `json:"notes"`
	Items              []DispatchItemRequest `json:"items"`
}

type UpdateStatusRequest struct {
	Status       string  `json:"dispatch_status"`
	DeliveryDate *string `json:"delivery_date"`
	Notes        *string `json:"notes"`
}

type DispatchItemRequest struct {
	OrderItemID *int64  `json:"order_item_id"`
	Quantity    float64 `json:"quantity"`
	Notes       *string `json:"notes"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type DispatchesResponse struct {
	Dispatches []Dispatch `json:"dispatches"`
	Pagination Pagination `json:"pagination"`
}

type DispatchResponse struct {
	Dispatch Dispatch `json:"dispatch"`
}

type DispatchItemResponse struct {
	Item DispatchItem `json:"item"`
}
