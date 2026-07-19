package admin

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/revocation"
	"github.com/redis/go-redis/v9"
)

var ErrForbidden = apperrs.ErrForbidden

type Service struct {
	repository Repository
	redis      redis.Cmdable
}

func NewService(r Repository, redisClients ...redis.Cmdable) *Service {
	var rdb redis.Cmdable
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	return &Service{repository: r, redis: rdb}
}
func (s *Service) Dashboard(ctx context.Context, a ActorContext) (Summary, error) {
	if !a.HasRole("ADMIN") && !a.HasRole("SUPER_ADMIN") {
		return Summary{}, ErrForbidden
	}
	return s.repository.Summary(ctx)
}

func (s *Service) ListUsers(ctx context.Context, a ActorContext, input ListUsersRequest) ([]User, Pagination, error) {
	if !a.HasRole("ADMIN") && !a.HasRole("SUPER_ADMIN") {
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
	if !a.HasRole("ADMIN") && !a.HasRole("SUPER_ADMIN") {
		return ErrForbidden
	}
	status := strings.ToUpper(strings.TrimSpace(req.Status))
	if status != "ACTIVE" && status != "SUSPENDED" && status != "DELETED" {
		return errors.New("invalid status: use ACTIVE, SUSPENDED, or DELETED")
	}
	if status == "SUSPENDED" && strings.TrimSpace(req.Reason) == "" {
		return errors.New("reason is required when suspending a user")
	}
	if err := s.repository.UpdateUserStatus(ctx, a.UserID, userID, status, strings.TrimSpace(req.Reason)); err != nil {
		return err
	}
	switch status {
	case "SUSPENDED", "DELETED":
		revocation.Revoke(userID, 20*time.Minute)
	case "ACTIVE":
		revocation.Remove(userID)
	}
	redisutil.InvalidateWorkspaces(ctx, s.redis, slog.Default(), userID)
	return nil
}

func (s *Service) UpdateNurseryStatus(ctx context.Context, a ActorContext, nurseryID int64, req UpdateNurseryStatusRequest) error {
	if !a.HasRole("ADMIN") && !a.HasRole("SUPER_ADMIN") {
		return ErrForbidden
	}
	status := strings.ToUpper(strings.TrimSpace(req.Status))
	if status != "ACTIVE" && status != "SUSPENDED" {
		return errors.New("invalid status: use ACTIVE or SUSPENDED")
	}
	if status == "SUSPENDED" && strings.TrimSpace(req.Reason) == "" {
		return errors.New("reason is required when suspending a nursery")
	}
	if err := s.repository.UpdateNurseryStatus(ctx, a.UserID, nurseryID, status, strings.TrimSpace(req.Reason)); err != nil {
		return err
	}
	userIDs, err := s.repository.WorkspaceUserIDs(ctx, nurseryID)
	if err != nil {
		slog.Warn("workspace invalidation user lookup failed", "nursery_id", nurseryID, "error", err)
		return nil
	}
	redisutil.InvalidateWorkspaces(ctx, s.redis, slog.Default(), userIDs...)
	return nil
}
