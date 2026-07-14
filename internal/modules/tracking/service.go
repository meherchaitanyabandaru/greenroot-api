package tracking

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisgeo"
)

var ErrInvalidInput = errors.New("invalid input")
var ErrForbidden = errors.New("forbidden")

type Service struct {
	repository Repository
	liveGeo    *redisgeo.Service
}

func NewService(r Repository, liveGeo ...*redisgeo.Service) *Service {
	s := &Service{repository: r}
	if len(liveGeo) > 0 {
		s.liveGeo = liveGeo[0]
	}
	return s
}
func (s *Service) Create(ctx context.Context, a ActorContext, in CreateRequest) (TrackingPoint, error) {
	if !hasRole(a, "ADMIN") && !hasRole(a, "DRIVER") {
		return TrackingPoint{}, ErrForbidden
	}
	if (in.VehicleID == nil && in.DriverID == nil && in.DispatchID == nil) || in.Latitude < -90 || in.Latitude > 90 || in.Longitude < -180 || in.Longitude > 180 {
		return TrackingPoint{}, ErrInvalidInput
	}
	if in.DispatchID != nil {
		if err := s.canTrackDispatch(ctx, a, *in.DispatchID); err != nil {
			return TrackingPoint{}, err
		}
	}
	p, err := s.repository.Create(ctx, in)
	if err != nil {
		return TrackingPoint{}, err
	}
	return *p, nil
}
func (s *Service) UpdateLiveLocation(ctx context.Context, a ActorContext, in LiveLocationRequest) (*LiveDriverLocation, error) {
	if !hasAnyRole(a, "ADMIN", "DRIVER") {
		return nil, ErrForbidden
	}
	if in.Latitude < -90 || in.Latitude > 90 || in.Longitude < -180 || in.Longitude > 180 {
		return nil, ErrInvalidInput
	}
	if in.DispatchID == nil || *in.DispatchID <= 0 {
		return nil, ErrInvalidInput
	}
	if err := s.canTrackDispatch(ctx, a, *in.DispatchID); err != nil {
		return nil, err
	}

	driverUserID := a.UserID
	if in.DriverUserID != nil {
		driverUserID = *in.DriverUserID
	}
	if driverUserID <= 0 {
		return nil, ErrInvalidInput
	}
	if hasRole(a, "DRIVER") && !hasRole(a, "ADMIN") && driverUserID != a.UserID {
		return nil, ErrForbidden
	}
	if s.liveGeo == nil {
		slog.Warn("redis geo live tracking unavailable", "driver_user_id", driverUserID, "error", redisgeo.ErrUnavailable)
		return nil, nil
	}

	loc, err := s.liveGeo.UpsertDriver(ctx, driverUserID, in.Latitude, in.Longitude)
	if err != nil {
		slog.Warn("redis geo live location update skipped", "driver_user_id", driverUserID, "error", err)
		return nil, nil
	}
	return toLiveLocation(loc), nil
}
func (s *Service) canTrackDispatch(ctx context.Context, a ActorContext, dispatchID int64) error {
	access, err := s.repository.DispatchAccess(ctx, dispatchID)
	if err != nil {
		return err
	}
	if !strings.EqualFold(access.Status, "IN_TRANSIT") {
		return ErrInvalidInput
	}
	if hasRole(a, "ADMIN") {
		return nil
	}
	if !hasRole(a, "DRIVER") {
		return ErrForbidden
	}
	if access.DriverUserID == nil || *access.DriverUserID != a.UserID {
		return ErrForbidden
	}
	return nil
}
func (s *Service) GetLiveDriver(ctx context.Context, a ActorContext, driverUserID int64) (*LiveDriverLocation, error) {
	if !hasAnyRole(a, "ADMIN", "DRIVER", "NURSERY_OWNER", "MANAGER") {
		return nil, ErrForbidden
	}
	if driverUserID <= 0 {
		return nil, ErrInvalidInput
	}
	if hasRole(a, "DRIVER") && !hasRole(a, "ADMIN") && driverUserID != a.UserID {
		return nil, ErrForbidden
	}
	if s.liveGeo == nil {
		slog.Warn("redis geo live tracking unavailable", "driver_user_id", driverUserID, "error", redisgeo.ErrUnavailable)
		return nil, nil
	}
	loc, err := s.liveGeo.GetDriver(ctx, driverUserID)
	if err != nil {
		slog.Warn("redis geo live location fetch skipped", "driver_user_id", driverUserID, "error", err)
		return nil, nil
	}
	return toLiveLocation(loc), nil
}
func (s *Service) NearbyLiveDrivers(ctx context.Context, a ActorContext, latitude, longitude, radiusKM float64, limit int) ([]NearbyLiveDriver, error) {
	if !hasAnyRole(a, "ADMIN", "NURSERY_OWNER", "MANAGER") {
		return nil, ErrForbidden
	}
	if latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 || radiusKM <= 0 {
		return nil, ErrInvalidInput
	}
	if s.liveGeo == nil {
		slog.Warn("redis geo nearby live drivers unavailable", "error", redisgeo.ErrUnavailable)
		return []NearbyLiveDriver{}, nil
	}
	rows, err := s.liveGeo.Nearby(ctx, latitude, longitude, radiusKM, limit)
	if err != nil {
		slog.Warn("redis geo nearby live drivers fetch skipped", "error", err)
		return []NearbyLiveDriver{}, nil
	}
	out := make([]NearbyLiveDriver, 0, len(rows))
	for _, row := range rows {
		out = append(out, NearbyLiveDriver{
			LiveDriverLocation: *toLiveLocation(&row.Location),
			DistanceKM:         row.DistanceKM,
		})
	}
	return out, nil
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
func toLiveLocation(loc *redisgeo.Location) *LiveDriverLocation {
	if loc == nil {
		return nil
	}
	return &LiveDriverLocation{
		DriverUserID: loc.DriverID,
		Latitude:     loc.Latitude,
		Longitude:    loc.Longitude,
		LastSeen:     loc.LastSeen.UTC().Format(time.RFC3339Nano),
	}
}
func hasRole(a ActorContext, role string) bool {
	for _, r := range a.Roles {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	return false
}
func hasAnyRole(a ActorContext, roles ...string) bool {
	for _, role := range roles {
		if hasRole(a, role) {
			return true
		}
	}
	return false
}
