package ratings

import (
	"context"
	"errors"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
)

var ErrForbidden = apperrs.ErrForbidden

// ErrNotDoneYet: an order is rated once completed, a trip once delivered.
var ErrNotDoneYet = errors.New("not completed yet")
var ErrInvalidInput = apperrs.ErrInvalidInput

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
	if !actor.HasRole("BUYER") && !actor.HasRole("NURSERY_OWNER") && !actor.HasRole("MANAGER") {
		return nil, ErrForbidden
	}
	if !scoresValid(req.DriverBehaviour, req.OnTimeDelivery, req.PlantCondition) {
		return nil, ErrInvalidInput
	}
	// Only the trip's customer or the selling nursery, once it is delivered.
	if err := allowed(s.repository.TripRatable(ctx, actor.UserID, dispatchID)); err != nil {
		return nil, err
	}
	return s.repository.UpsertTrip(ctx, actor.UserID, dispatchID, req)
}

func (s *Service) SubmitOrder(ctx context.Context, actor ActorContext, orderID int64, req SubmitOrderRatingRequest) (*Rating, error) {
	// Only buyers can rate orders
	if !actor.HasRole("BUYER") {
		return nil, ErrForbidden
	}
	if !scoresValid(req.PlantQuality, req.Communication, req.OverallExperience) {
		return nil, ErrInvalidInput
	}
	// Only the order's own buyer, once it is completed.
	if err := allowed(s.repository.OrderRatable(ctx, actor.UserID, orderID)); err != nil {
		return nil, err
	}
	return s.repository.UpsertOrder(ctx, actor.UserID, orderID, req)
}

func allowed(found, party, done bool, err error) error {
	switch {
	case err != nil:
		return err
	case !found:
		return ErrNotFound
	case !party:
		return ErrForbidden
	case !done:
		return ErrNotDoneYet
	}
	return nil
}

// scoresValid: each given score is 1-5 (the DB check would otherwise fail as a 500).
func scoresValid(scores ...*int) bool {
	for _, v := range scores {
		if v != nil && (*v < 1 || *v > 5) {
			return false
		}
	}
	return true
}

func (s *Service) GetMyRatings(ctx context.Context, actor ActorContext) (map[string]any, error) {
	result := map[string]any{}
	if r, err := s.repository.GetApp(ctx, actor.UserID); err == nil {
		result["app"] = r
	}
	return result, nil
}

// GetMyAppRating is the caller's own app rating, or nil before they rate.
func (s *Service) GetMyAppRating(ctx context.Context, actor ActorContext) (*Rating, error) {
	r, err := s.repository.GetApp(ctx, actor.UserID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return r, err
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
	if !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		return nil, ErrForbidden
	}
	return s.repository.List(ctx, input)
}
