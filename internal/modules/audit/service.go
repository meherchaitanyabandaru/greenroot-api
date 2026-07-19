package audit

import (
	"context"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
)

var ErrForbidden = apperrs.ErrForbidden

type Service struct{ repository Repository }

func NewService(r Repository) *Service { return &Service{repository: r} }
func (s *Service) List(ctx context.Context, actor ActorContext, in ListRequest) ([]AuditLog, Pagination, error) {
	if !actor.HasRole("ADMIN") {
		return nil, Pagination{}, ErrForbidden
	}
	in = normalize(in)
	rows, total, err := s.repository.List(ctx, in)
	if err != nil {
		return nil, Pagination{}, err
	}
	return rows, Pagination{Page: in.Page, PerPage: in.PerPage, Total: total, TotalPages: pages(total, in.PerPage)}, nil
}
func (s *Service) ListSecurity(ctx context.Context, actor ActorContext, in ListSecurityRequest) ([]SecurityLog, Pagination, error) {
	if !actor.HasRole("ADMIN") {
		return nil, Pagination{}, ErrForbidden
	}
	if in.Page <= 0 {
		in.Page = 1
	}
	if in.PerPage <= 0 {
		in.PerPage = 20
	}
	if in.PerPage > 100 {
		in.PerPage = 100
	}
	rows, total, err := s.repository.ListSecurity(ctx, in)
	if err != nil {
		return nil, Pagination{}, err
	}
	return rows, Pagination{Page: in.Page, PerPage: in.PerPage, Total: total, TotalPages: pages(total, in.PerPage)}, nil
}
