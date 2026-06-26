package tracking

import (
	"context"
	"errors"
	"strings"
)

var ErrInvalidInput = errors.New("invalid input")
var ErrForbidden = errors.New("forbidden")

type Service struct{ repository Repository }

func NewService(r Repository) *Service { return &Service{repository: r} }
func (s *Service) Create(ctx context.Context, a ActorContext, in CreateRequest) (TrackingPoint, error) {
	if !hasRole(a, "ADMIN") && !hasRole(a, "DRIVER") {
		return TrackingPoint{}, ErrForbidden
	}
	if (in.VehicleID == nil && in.DriverID == nil && in.DispatchID == nil) || in.Latitude < -90 || in.Latitude > 90 || in.Longitude < -180 || in.Longitude > 180 {
		return TrackingPoint{}, ErrInvalidInput
	}
	p, err := s.repository.Create(ctx, in)
	if err != nil {
		return TrackingPoint{}, err
	}
	return *p, nil
}
func (s *Service) List(ctx context.Context, a ActorContext, col string, id int64) ([]TrackingPoint, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	return s.repository.ListBy(ctx, col, id)
}
func (s *Service) Latest(ctx context.Context, a ActorContext, col string, id int64) (*TrackingPoint, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	return s.repository.LatestBy(ctx, col, id)
}
func hasRole(a ActorContext, role string) bool {
	for _, r := range a.Roles {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	return false
}
