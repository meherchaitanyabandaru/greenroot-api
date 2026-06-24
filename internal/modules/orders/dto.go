package orders

type ListOrdersRequest struct {
	Page      int
	PerPage   int
	Search    string
	BuyerID   int64
	NurseryID int64
	Status    string
	SortBy    string
	SortOrder string
}

type CreateOrderRequest struct {
	OrderNumber     *string            `json:"order_number"`
	BuyerUserID     *int64             `json:"buyer_user_id"`
	BuyerMobile     *string            `json:"buyer_mobile"`
	BuyerName       *string            `json:"buyer_name"`
	SellerNurseryID *int64             `json:"seller_nursery_id"`
	Status          string             `json:"order_status"`
	Notes           *string            `json:"notes"`
	Items           []OrderItemRequest `json:"items"`
}

type UpdateStatusRequest struct {
	Status string `json:"order_status"`
}

type OrderItemRequest struct {
	PlantID    int64   `json:"plant_id"`
	SizeID     *int16  `json:"size_id"`
	Quantity   float64 `json:"quantity"`
	UnitPrice  float64 `json:"unit_price"`
	TotalPrice float64 `json:"total_price"`
	Remarks    *string `json:"remarks"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type OrdersResponse struct {
	Orders     []Order    `json:"orders"`
	Pagination Pagination `json:"pagination"`
}

type OrderResponse struct {
	Order Order `json:"order"`
}

type ItemsResponse struct {
	Items []OrderItem `json:"items"`
}

type ItemResponse struct {
	Item OrderItem `json:"item"`
}

type MessageResponse struct {
	Message string `json:"message"`
}
