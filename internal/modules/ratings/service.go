package ratings

import (
	"context"
	"errors"
	"strings"
)

var ErrForbidden = errors.New("forbidden")
var ErrInvalidInput = errors.New("invalid input")

type Service struct {
	repository Repository
}

func NewService(repo Repository) *Service {
	return &Service{repository: repo}
}

func (s *Service) SubmitApp(ctx context.Context, actor ActorContext, req SubmitAppRatingRequest) (*Rating, error) {
	if req.OverallRating < 1 || req.OverallRating > 5 {
		return nil, ErrInvalidInput
	}
	return s.repository.UpsertApp(ctx, actor.UserID, req)
}

func (s *Service) SubmitTrip(ctx context.Context, actor ActorContext, dispatchID int64, req SubmitTripRatingRequest) (*Rating, error) {
	if !hasRole(actor, "BUYER") && !hasRole(actor, "NURSERY_OWNER") && !hasRole(actor, "MANAGER") {
		return nil, ErrForbidden
	}
	return s.repository.UpsertTrip(ctx, actor.UserID, dispatchID, req)
}

func (s *Service) SubmitOrder(ctx context.Context, actor ActorContext, orderID int64, req SubmitOrderRatingRequest) (*Rating, error) {
	// Only buyers can rate orders
	if !hasRole(actor, "BUYER") {
		return nil, ErrForbidden
	}
	return s.repository.UpsertOrder(ctx, actor.UserID, orderID, req)
}

func (s *Service) GetMyRatings(ctx context.Context, actor ActorContext) (map[string]any, error) {
	result := map[string]any{}
	if r, err := s.repository.GetApp(ctx, actor.UserID); err == nil {
		result["app"] = r
	}
	return result, nil
}

func (s *Service) GetMyOrderRating(ctx context.Context, actor ActorContext, orderID int64) (*Rating, error) {
	r, err := s.repository.GetOrder(ctx, actor.UserID, orderID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return r, err
}

func (s *Service) GetMyTripRating(ctx context.Context, actor ActorContext, dispatchID int64) (*Rating, error) {
	r, err := s.repository.GetTrip(ctx, actor.UserID, dispatchID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return r, err
}

func (s *Service) List(ctx context.Context, actor ActorContext, input ListRatingsRequest) ([]Rating, error) {
	if !hasRole(actor, "ADMIN") && !hasRole(actor, "SUPER_ADMIN") {
		return nil, ErrForbidden
	}
	return s.repository.List(ctx, input)
}

func hasRole(actor ActorContext, role string) bool {
	for _, r := range actor.Roles {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	return false
}
