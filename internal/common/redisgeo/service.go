package redisgeo

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	KeyDriverLiveGeo      = "geo:drivers:live"
	KeyDriverLiveLastSeen = "geo:drivers:live:last_seen:"
	DefaultLastSeenTTL    = 90 * time.Second
)

var ErrUnavailable = errors.New("redis geo unavailable")

type Service struct {
	redis redis.Cmdable
	ttl   time.Duration
}

type Option func(*Service)

func WithLastSeenTTL(ttl time.Duration) Option {
	return func(s *Service) {
		if ttl > 0 {
			s.ttl = ttl
		}
	}
}

type Location struct {
	DriverID  int64     `json:"driver_id"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	LastSeen  time.Time `json:"last_seen"`
}

type NearbyLocation struct {
	Location
	DistanceKM float64 `json:"distance_km"`
}

func New(rdb redis.Cmdable, opts ...Option) *Service {
	s := &Service{redis: rdb, ttl: DefaultLastSeenTTL}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) UpsertDriver(ctx context.Context, driverID int64, latitude, longitude float64) (*Location, error) {
	if err := s.validate(driverID, latitude, longitude); err != nil {
		return nil, err
	}
	if s.redis == nil {
		return nil, ErrUnavailable
	}

	member := memberName(driverID)
	now := time.Now().UTC()
	if err := s.redis.GeoAdd(ctx, KeyDriverLiveGeo, &redis.GeoLocation{
		Name:      member,
		Longitude: longitude,
		Latitude:  latitude,
	}).Err(); err != nil {
		return nil, err
	}
	if err := s.redis.Set(ctx, lastSeenKey(driverID), now.Format(time.RFC3339Nano), s.ttl).Err(); err != nil {
		return nil, err
	}
	return &Location{DriverID: driverID, Latitude: latitude, Longitude: longitude, LastSeen: now}, nil
}

func (s *Service) GetDriver(ctx context.Context, driverID int64) (*Location, error) {
	if driverID <= 0 {
		return nil, fmt.Errorf("driver_id must be positive")
	}
	if s.redis == nil {
		return nil, ErrUnavailable
	}

	lastSeen, ok, err := s.lastSeen(ctx, driverID)
	if err != nil || !ok {
		return nil, err
	}
	positions, err := s.redis.GeoPos(ctx, KeyDriverLiveGeo, memberName(driverID)).Result()
	if err != nil {
		return nil, err
	}
	if len(positions) == 0 || positions[0] == nil {
		_ = s.redis.Del(ctx, lastSeenKey(driverID)).Err()
		return nil, nil
	}
	return &Location{
		DriverID:  driverID,
		Latitude:  positions[0].Latitude,
		Longitude: positions[0].Longitude,
		LastSeen:  lastSeen,
	}, nil
}

func (s *Service) Nearby(ctx context.Context, latitude, longitude, radiusKM float64, limit int) ([]NearbyLocation, error) {
	if radiusKM <= 0 {
		return nil, fmt.Errorf("radius_km must be positive")
	}
	if limit <= 0 {
		limit = 50
	}
	if err := validateCoordinate(latitude, longitude); err != nil {
		return nil, err
	}
	if s.redis == nil {
		return nil, ErrUnavailable
	}

	rows, err := s.redis.GeoRadius(ctx, KeyDriverLiveGeo, longitude, latitude, &redis.GeoRadiusQuery{
		Radius:    radiusKM,
		Unit:      "km",
		Sort:      "ASC",
		Count:     limit,
		WithCoord: true,
		WithDist:  true,
	}).Result()
	if err != nil {
		return nil, err
	}

	out := make([]NearbyLocation, 0, len(rows))
	for _, row := range rows {
		driverID, err := strconv.ParseInt(row.Name, 10, 64)
		if err != nil || driverID <= 0 {
			continue
		}
		lastSeen, ok, err := s.lastSeen(ctx, driverID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, NearbyLocation{
			Location: Location{
				DriverID:  driverID,
				Latitude:  row.Latitude,
				Longitude: row.Longitude,
				LastSeen:  lastSeen,
			},
			DistanceKM: row.Dist,
		})
	}
	return out, nil
}

func (s *Service) RemoveDriver(ctx context.Context, driverID int64) error {
	if driverID <= 0 {
		return fmt.Errorf("driver_id must be positive")
	}
	if s.redis == nil {
		return ErrUnavailable
	}
	if err := s.redis.ZRem(ctx, KeyDriverLiveGeo, memberName(driverID)).Err(); err != nil {
		return err
	}
	return s.redis.Del(ctx, lastSeenKey(driverID)).Err()
}

func (s *Service) validate(driverID int64, latitude, longitude float64) error {
	if driverID <= 0 {
		return fmt.Errorf("driver_id must be positive")
	}
	return validateCoordinate(latitude, longitude)
}

func validateCoordinate(latitude, longitude float64) error {
	if latitude < -90 || latitude > 90 {
		return fmt.Errorf("latitude out of range")
	}
	if longitude < -180 || longitude > 180 {
		return fmt.Errorf("longitude out of range")
	}
	return nil
}

func (s *Service) lastSeen(ctx context.Context, driverID int64) (time.Time, bool, error) {
	value, err := s.redis.Get(ctx, lastSeenKey(driverID)).Result()
	if errors.Is(err, redis.Nil) {
		_ = s.redis.ZRem(ctx, KeyDriverLiveGeo, memberName(driverID)).Err()
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	seen, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		_ = s.redis.Del(ctx, lastSeenKey(driverID)).Err()
		_ = s.redis.ZRem(ctx, KeyDriverLiveGeo, memberName(driverID)).Err()
		return time.Time{}, false, nil
	}
	return seen, true, nil
}

func memberName(driverID int64) string {
	return strconv.FormatInt(driverID, 10)
}

func lastSeenKey(driverID int64) string {
	return KeyDriverLiveLastSeen + memberName(driverID)
}
