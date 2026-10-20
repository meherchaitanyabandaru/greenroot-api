package sourcing

import (
	"context"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
)

var (
	ErrForbidden    = apperrs.ErrForbidden
	ErrInvalidInput = apperrs.ErrInvalidInput
)

type Service struct {
	repository Repository
	auditSvc   *auditlog.Service
}

func NewService(r Repository, auditSvc *auditlog.Service) *Service {
	return &Service{repository: r, auditSvc: auditSvc}
}

// canSource returns true for roles that may use the Plant Sourcing Network.
// Per business rules: NURSERY_OWNER and MANAGER have full sourcing access.
// ADMIN/SUPER_ADMIN can monitor. DRIVER and BUYER have no access.
func canSource(actor ActorContext) bool {
	return actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") ||
		actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER")
}

// canManageNursery returns nil if the actor may write to the given nursery.
func (s *Service) canManageNursery(ctx context.Context, actor ActorContext, nurseryID int64) error {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return nil
	}
	ok, err := s.repository.IsNurseryMember(ctx, nurseryID, actor.UserID)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return ErrForbidden
}

// ---- Network membership ----

func (s *Service) GetMembership(ctx context.Context, actor ActorContext, nurseryID int64) (Member, error) {
	if !canSource(actor) {
		return Member{}, ErrForbidden
	}
	m, err := s.repository.GetMember(ctx, nurseryID)
	if err != nil {
		return Member{}, err
	}
	return *m, nil
}

func (s *Service) JoinNetwork(ctx context.Context, actor ActorContext, nurseryID int64, req JoinNetworkRequest) (Member, error) {
	if err := s.canManageNursery(ctx, actor, nurseryID); err != nil {
		return Member{}, err
	}
	m, err := s.repository.JoinNetwork(ctx, nurseryID, actor.UserID, req)
	if err != nil {
		return Member{}, err
	}
	s.audit(ctx, actor, "sourcing_network_members", m.ID, actionInsert, req)
	return *m, nil
}

func (s *Service) LeaveNetwork(ctx context.Context, actor ActorContext, nurseryID int64) error {
	if err := s.canManageNursery(ctx, actor, nurseryID); err != nil {
		return err
	}
	if err := s.repository.LeaveNetwork(ctx, nurseryID); err != nil {
		return err
	}
	s.audit(ctx, actor, "sourcing_network_members", nurseryID, actionUpdate, map[string]any{"is_active": false})
	return nil
}

// ---- Discovery ----

func (s *Service) ListNearby(ctx context.Context, actor ActorContext, q NearbyQuery) ([]NearbyNursery, Pagination, error) {
	if !canSource(actor) {
		return nil, Pagination{}, ErrForbidden
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PerPage <= 0 {
		q.PerPage = 20
	}
	nurseries, total, err := s.repository.ListNearby(ctx, q)
	if err != nil {
		return nil, Pagination{}, err
	}
	return nurseries, pagination(total, q.Page, q.PerPage), nil
}

func (s *Service) GetNurseryProfile(ctx context.Context, actor ActorContext, nurseryID int64) (NearbyNursery, error) {
	if !canSource(actor) {
		return NearbyNursery{}, ErrForbidden
	}
	nn, err := s.repository.GetNurseryProfile(ctx, nurseryID)
	if err != nil {
		return NearbyNursery{}, err
	}
	return *nn, nil
}

// ---- Sourcing posts ----

// ---- Post responses ----

// ---- Helpers ----

func pagination(total int64, page, perPage int) Pagination {
	tp := 0
	if perPage > 0 {
		tp = int((total + int64(perPage) - 1) / int64(perPage))
	}
	return Pagination{Page: page, PerPage: perPage, Total: total, TotalPages: tp}
}

func (s *Service) audit(ctx context.Context, actor ActorContext, entityType string, entityID int64, action auditlog.Action, newValue any) {
	s.auditSvc.Log(ctx, auditlog.Entry{
		UserID:     actor.UserID,
		Module:     auditlog.ModuleSourcing,
		EntityType: entityType,
		EntityID:   entityID,
		Action:     action,
		NewValue:   newValue,
		IPAddress:  actor.IPAddress,
		DeviceInfo: actor.UserAgent,
	})
}
