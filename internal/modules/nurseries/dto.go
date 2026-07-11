package nurseries

type ListNurseriesRequest struct {
	Page               int
	PerPage            int
	Search             string
	City               string
	State              string
	NurseryStatus      string
	VerificationStatus string
}

type CreateNurseryRequest struct {
	Code        *string `json:"code"`
	Name        string  `json:"name"`
	GSTNumber   *string `json:"gst_number"`
	Mobile      *string `json:"mobile"`
	Email       *string `json:"email"`
	Website     *string `json:"website"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
	OwnerUserID *int64  `json:"owner_user_id"`
}

// UpdateNurseryRequest extends create fields with branding.
// logo_url and brand_icon_key are mutually exclusive; setting one clears the other.
// brand_color must be one of the 10 curated palette values (validated in service).
type UpdateNurseryRequest struct {
	Code        *string `json:"code"`
	Name        string  `json:"name"`
	GSTNumber   *string `json:"gst_number"`
	Mobile      *string `json:"mobile"`
	Email       *string `json:"email"`
	Website     *string `json:"website"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
	OwnerUserID *int64  `json:"owner_user_id"`
	LogoURL     *string `json:"logo_url"`
	BrandIconKey *string `json:"brand_icon_key"`
	BrandColor  *string `json:"brand_color"`
}

type AddressRequest struct {
	AddressType  *string  `json:"address_type"`
	AddressLine1 *string  `json:"address_line1"`
	AddressLine2 *string  `json:"address_line2"`
	City         *string  `json:"city"`
	State        *string  `json:"state"`
	Country      *string  `json:"country"`
	PostalCode   *string  `json:"postal_code"`
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
	IsPrimary    bool     `json:"is_primary"`
}

type AddUserRequest struct {
	UserID   int64  `json:"user_id"`
	RoleID   int16  `json:"role_id"`
	RoleCode string `json:"role_code"`
}

type AddManagerRequest struct {
	UserID int64  `json:"user_id"`
	Role   string `json:"role"` // MANAGER | GUMASTHA
}

type ConnectDriverRequest struct {
	DriverUserID int64 `json:"driver_user_id"`
}

type DriversResponse struct {
	Drivers []NurseryDriver `json:"drivers"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type NurseriesResponse struct {
	Nurseries  []Nursery  `json:"nurseries"`
	Pagination Pagination `json:"pagination"`
}

type NurseryResponse struct {
	Nursery Nursery `json:"nursery"`
}

type AddressesResponse struct {
	Addresses []Address `json:"addresses"`
}

type AddressResponse struct {
	Address Address `json:"address"`
}

type UsersResponse struct {
	Users []UserLink `json:"users"`
}

type UserResponse struct {
	User UserLink `json:"user"`
}

type MessageResponse struct {
	Message string `json:"message"`
}

type CustomersResponse struct {
	Customers []Customer `json:"customers"`
}
