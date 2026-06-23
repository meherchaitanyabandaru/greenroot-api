package vehicles

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
)

var (
	ErrForbidden    = errors.New("forbidden")
	ErrInvalidInput = errors.New("invalid input")
	ErrDuplicate    = errors.New("duplicate vehicle")
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(ctx context.Context, actor ActorContext, input ListVehiclesRequest) ([]Vehicle, Pagination, error) {
	if !hasRole(actor, "ADMIN") {
		return nil, Pagination{}, ErrForbidden
	}
	input = normalizeList(input)
	vehicles, total, err := s.repository.List(ctx, input)
	if err != nil {
		return nil, Pagination{}, err
	}
	return vehicles, Pagination{Page: input.Page, PerPage: input.PerPage, Total: total, TotalPages: totalPages(total, input.PerPage)}, nil
}

func (s *Service) Get(ctx context.Context, actor ActorContext, vehicleID int64) (Vehicle, error) {
	if !hasRole(actor, "ADMIN") {
		return Vehicle{}, ErrForbidden
	}
	vehicle, err := s.repository.FindByID(ctx, vehicleID)
	if err != nil {
		return Vehicle{}, err
	}
	return *vehicle, nil
}

func (s *Service) Create(ctx context.Context, actor ActorContext, input VehicleRequest) (Vehicle, error) {
	if !hasRole(actor, "ADMIN") {
		return Vehicle{}, ErrForbidden
	}
	if err := validate(input); err != nil {
		return Vehicle{}, err
	}
	duplicate, err := s.repository.HasDuplicate(ctx, input.VehicleNumber, 0)
	if err != nil {
		return Vehicle{}, err
	}
	if duplicate {
		return Vehicle{}, ErrDuplicate
	}
	vehicle, err := s.repository.Create(ctx, input)
	if err != nil {
		return Vehicle{}, err
	}
	s.audit(ctx, actor, vehicle.ID, actionInsert, input)
	return *vehicle, nil
}

func (s *Service) Update(ctx context.Context, actor ActorContext, vehicleID int64, input VehicleRequest) (Vehicle, error) {
	if !hasRole(actor, "ADMIN") {
		return Vehicle{}, ErrForbidden
	}
	if err := validate(input); err != nil {
		return Vehicle{}, err
	}
	duplicate, err := s.repository.HasDuplicate(ctx, input.VehicleNumber, vehicleID)
	if err != nil {
		return Vehicle{}, err
	}
	if duplicate {
		return Vehicle{}, ErrDuplicate
	}
	vehicle, err := s.repository.Update(ctx, vehicleID, input)
	if err != nil {
		return Vehicle{}, err
	}
	s.audit(ctx, actor, vehicle.ID, actionUpdate, input)
	return *vehicle, nil
}

func (s *Service) Delete(ctx context.Context, actor ActorContext, vehicleID int64) error {
	if !hasRole(actor, "ADMIN") {
		return ErrForbidden
	}
	if err := s.repository.Delete(ctx, vehicleID); err != nil {
		return err
	}
	s.audit(ctx, actor, vehicleID, actionDelete, map[string]any{"status": "RETIRED"})
	return nil
}

func validate(input VehicleRequest) error {
	if strings.TrimSpace(input.VehicleNumber) == "" || !isAllowedStatus(statusOrActive(input.Status)) {
		return ErrInvalidInput
	}
	if input.CapacityKG != nil && *input.CapacityKG < 0 {
		return ErrInvalidInput
	}
	return nil
}

func normalizeList(input ListVehiclesRequest) ListVehiclesRequest {
	if input.Page <= 0 {
		input.Page = 1
	}
	if input.PerPage <= 0 {
		input.PerPage = 20
	}
	if input.PerPage > 100 {
		input.PerPage = 100
	}
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	input.Type = strings.TrimSpace(input.Type)
	input.Search = strings.TrimSpace(input.Search)
	input.SortBy = strings.TrimSpace(input.SortBy)
	input.SortOrder = strings.ToLower(strings.TrimSpace(input.SortOrder))
	if input.SortOrder != "asc" && input.SortOrder != "desc" {
		input.SortOrder = "desc"
	}
	return input
}

func isAllowedStatus(status string) bool {
	switch status {
	case "ACTIVE", "INACTIVE", "MAINTENANCE", "RETIRED":
		return true
	default:
		return false
	}
}

func hasRole(actor ActorContext, role string) bool {
	for _, current := range actor.Roles {
		if strings.EqualFold(current, role) {
			return true
		}
	}
	return false
}

func totalPages(total int64, perPage int) int {
	if perPage <= 0 {
		return 0
	}
	return int(math.Ceil(float64(total) / float64(perPage)))
}

func (s *Service) audit(ctx context.Context, actor ActorContext, recordID int64, action string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = s.repository.CreateAuditLog(ctx, CreateAuditInput{
		TableName: "vehicles",
		RecordID:  recordID,
		Action:    action,
		ChangedBy: actor.UserID,
		SourceIP:  actor.IPAddress,
		UserAgent: actor.UserAgent,
		NewJSON:   string(data),
		At:        time.Now(),
	})
}
