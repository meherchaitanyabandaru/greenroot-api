package admin

import (
	"context"
	"errors"
	"strings"
)

var ErrForbidden = errors.New("forbidden")

type Service struct{ repository Repository }

func NewService(r Repository) *Service { return &Service{repository: r} }
func (s *Service) Dashboard(ctx context.Context, a ActorContext) (Summary, error) {
	if !hasRole(a, "ADMIN") && !hasRole(a, "SUPER_ADMIN") {
		return Summary{}, ErrForbidden
	}
	return s.repository.Summary(ctx)
}

func (s *Service) ListUsers(ctx context.Context, a ActorContext, input ListUsersRequest) ([]User, Pagination, error) {
	if !hasRole(a, "ADMIN") && !hasRole(a, "SUPER_ADMIN") {
		return nil, Pagination{}, ErrForbidden
	}
	users, total, err := s.repository.ListUsers(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	page, perPage := normalizePagination(input.Page, input.PerPage)
	return users, Pagination{
		Page:       page,
		PerPage:    perPage,
		Total:      total,
		TotalPages: int((total + int64(perPage) - 1) / int64(perPage)),
	}, nil
}

func hasRole(a ActorContext, role string) bool {
	for _, r := range a.Roles {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	return false
}
