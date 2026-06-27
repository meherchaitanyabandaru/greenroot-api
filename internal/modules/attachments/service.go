package attachments

import (
	"context"
	"errors"
	"math"
	"strings"
)

var (
	ErrForbidden    = errors.New("forbidden")
	ErrInvalidInput = errors.New("invalid input")
)

type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

func (s *Service) List(ctx context.Context, actor ActorContext, input ListRequest) ([]Attachment, Pagination, error) {
	input = normalizeList(input)
	items, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	return items, Pagination{Page: input.Page, PerPage: input.PerPage, Total: total, TotalPages: pages(total, input.PerPage)}, nil
}
func (s *Service) Get(ctx context.Context, actor ActorContext, id int64) (Attachment, error) {
	item, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return Attachment{}, err
	}
	return *item, nil
}
func (s *Service) Create(ctx context.Context, actor ActorContext, input AttachmentRequest) (Attachment, error) {
	// Only nursery staff and admins may upload attachments; buyers and unauthenticated users cannot
	if !hasRole(actor, "ADMIN") && !hasRole(actor, "SUPER_ADMIN") &&
		!hasRole(actor, "NURSERY_OWNER") && !hasRole(actor, "MANAGER") && !hasRole(actor, "DRIVER") {
		return Attachment{}, ErrForbidden
	}
	if strings.TrimSpace(input.EntityType) == "" || input.EntityID <= 0 || strings.TrimSpace(input.FileName) == "" || strings.TrimSpace(input.FileURL) == "" {
		return Attachment{}, ErrInvalidInput
	}
	input.EntityType = strings.ToUpper(strings.TrimSpace(input.EntityType))
	item, err := s.repository.Create(ctx, actor.UserID, input)
	if err != nil {
		return Attachment{}, err
	}
	return *item, nil
}
func (s *Service) Delete(ctx context.Context, actor ActorContext, id int64) error {
	if !hasRole(actor, "ADMIN") {
		return ErrForbidden
	}
	return s.repository.Delete(ctx, id)
}

func normalizeList(input ListRequest) ListRequest {
	if input.Page <= 0 {
		input.Page = 1
	}
	if input.PerPage <= 0 {
		input.PerPage = 20
	}
	if input.PerPage > 100 {
		input.PerPage = 100
	}
	input.EntityType = strings.ToUpper(strings.TrimSpace(input.EntityType))
	return input
}
func hasRole(actor ActorContext, role string) bool {
	for _, r := range actor.Roles {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	return false
}
func pages(total int64, per int) int {
	if per <= 0 {
		return 0
	}
	return int(math.Ceil(float64(total) / float64(per)))
}
