package admin

type DashboardResponse struct {
	Summary Summary `json:"summary"`
}

type ListUsersRequest struct {
	Page      int
	PerPage   int
	Search    string
	Status    string // one value or a comma list
	Role      string // one value or a comma list
	SortBy    string
	SortOrder string
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type UsersResponse struct {
	Users      []User     `json:"users"`
	Pagination Pagination `json:"pagination"`
}
