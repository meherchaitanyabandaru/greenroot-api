package admin

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/revocation"
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

// UpdateUserStatus changes a user's status. On SUSPENDED/DELETED, the user is
// immediately revoked from the in-process store so their next request is rejected
// within milliseconds — even if their JWT is still within its 15-minute window.
func (s *Service) UpdateUserStatus(ctx context.Context, a ActorContext, userID int64, req UpdateUserStatusRequest) error {
	if !hasRole(a, "ADMIN") && !hasRole(a, "SUPER_ADMIN") {
		return ErrForbidden
	}
	status := strings.ToUpper(strings.TrimSpace(req.Status))
	if status != "ACTIVE" && status != "SUSPENDED" && status != "DELETED" {
		return errors.New("invalid status: use ACTIVE, SUSPENDED, or DELETED")
	}
	if err := s.repository.UpdateUserStatus(ctx, userID, status); err != nil {
		return err
	}
	switch status {
	case "SUSPENDED", "DELETED":
		revocation.Revoke(userID, 20*time.Minute)
	case "ACTIVE":
		revocation.Remove(userID) // lift revocation immediately on reinstatement
	}
	return nil
}

func (s *Service) UpdateNurseryStatus(ctx context.Context, a ActorContext, nurseryID int64, req UpdateNurseryStatusRequest) error {
	if !hasRole(a, "ADMIN") && !hasRole(a, "SUPER_ADMIN") {
		return ErrForbidden
	}
	status := strings.ToUpper(strings.TrimSpace(req.Status))
	if status != "ACTIVE" && status != "SUSPENDED" {
		return errors.New("invalid status: use ACTIVE or SUSPENDED")
	}
	return s.repository.UpdateNurseryStatus(ctx, nurseryID, status)
}

func hasRole(a ActorContext, role string) bool {
	for _, r := range a.Roles {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	return false
}
