package admin

import "github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"

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

type Pagination = response.Pagination

type UsersResponse struct {
	Users      []User     `json:"users"`
	Pagination Pagination `json:"pagination"`
}
