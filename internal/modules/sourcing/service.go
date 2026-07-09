package sourcing

import (
	"context"
	"errors"
	"strings"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
)

var (
	ErrForbidden    = errors.New("forbidden")
	ErrInvalidInput = errors.New("invalid input")
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
	return hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") ||
		hasRole(actor, "NURSERY_OWNER") || hasRole(actor, "MANAGER")
}

// canManageNursery returns nil if the actor may write to the given nursery.
func (s *Service) canManageNursery(ctx context.Context, actor ActorContext, nurseryID int64) error {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
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

// ---- Featured plants ----

func (s *Service) ListFeaturedPlants(ctx context.Context, actor ActorContext, nurseryID int64) ([]FeaturedPlant, error) {
	if !canSource(actor) {
		return nil, ErrForbidden
	}
	return s.repository.ListFeaturedPlants(ctx, nurseryID)
}

func (s *Service) AddFeaturedPlant(ctx context.Context, actor ActorContext, nurseryID int64, req CreateFeaturedPlantRequest) (FeaturedPlant, error) {
	if err := s.canManageNursery(ctx, actor, nurseryID); err != nil {
		return FeaturedPlant{}, err
	}
	if req.PlantID <= 0 {
		return FeaturedPlant{}, ErrInvalidInput
	}
	fp, err := s.repository.AddFeaturedPlant(ctx, nurseryID, actor.UserID, req)
	if err != nil {
		return FeaturedPlant{}, err
	}
	s.audit(ctx, actor, "nursery_featured_plants", fp.ID, actionInsert, req)
	return *fp, nil
}

func (s *Service) UpdateFeaturedPlant(ctx context.Context, actor ActorContext, nurseryID int64, featuredID int64, req UpdateFeaturedPlantRequest) (FeaturedPlant, error) {
	if err := s.canManageNursery(ctx, actor, nurseryID); err != nil {
		return FeaturedPlant{}, err
	}
	fp, err := s.repository.UpdateFeaturedPlant(ctx, featuredID, req)
	if err != nil {
		return FeaturedPlant{}, err
	}
	s.audit(ctx, actor, "nursery_featured_plants", featuredID, actionUpdate, req)
	return *fp, nil
}

func (s *Service) DeleteFeaturedPlant(ctx context.Context, actor ActorContext, nurseryID int64, featuredID int64) error {
	if err := s.canManageNursery(ctx, actor, nurseryID); err != nil {
		return err
	}
	if err := s.repository.DeleteFeaturedPlant(ctx, featuredID); err != nil {
		return err
	}
	s.audit(ctx, actor, "nursery_featured_plants", featuredID, actionDelete, nil)
	return nil
}

// ---- Sourcing posts ----

func (s *Service) ListPosts(ctx context.Context, actor ActorContext, q ListPostsQuery) ([]SourcingPost, Pagination, error) {
	if !canSource(actor) {
		return nil, Pagination{}, ErrForbidden
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PerPage <= 0 {
		q.PerPage = 20
	}
	q.PostType = strings.ToUpper(strings.TrimSpace(q.PostType))
	q.Status = strings.ToUpper(strings.TrimSpace(q.Status))
	posts, total, err := s.repository.ListPosts(ctx, q)
	if err != nil {
		return nil, Pagination{}, err
	}
	return posts, pagination(total, q.Page, q.PerPage), nil
}

func (s *Service) GetPost(ctx context.Context, actor ActorContext, postID int64) (SourcingPost, error) {
	if !canSource(actor) {
		return SourcingPost{}, ErrForbidden
	}
	p, err := s.repository.GetPost(ctx, postID)
	if err != nil {
		return SourcingPost{}, err
	}
	return *p, nil
}

func (s *Service) CreatePost(ctx context.Context, actor ActorContext, req CreatePostRequest) (SourcingPost, error) {
	req.PostType = strings.ToUpper(strings.TrimSpace(req.PostType))
	req.Urgency = strings.ToUpper(strings.TrimSpace(req.Urgency))
	if req.Urgency == "NORMAL" {
		req.Urgency = "FLEXIBLE"
	}
	if err := s.canManageNursery(ctx, actor, req.NurseryID); err != nil {
		return SourcingPost{}, err
	}
	if err := validatePostRequest(req); err != nil {
		return SourcingPost{}, err
	}
	p, err := s.repository.CreatePost(ctx, actor.UserID, req)
	if err != nil {
		return SourcingPost{}, err
	}
	s.audit(ctx, actor, "sourcing_posts", p.ID, actionInsert, req)
	return *p, nil
}

func (s *Service) UpdatePost(ctx context.Context, actor ActorContext, postID int64, req UpdatePostRequest) (SourcingPost, error) {
	existing, err := s.repository.GetPost(ctx, postID)
	if err != nil {
		return SourcingPost{}, err
	}
	if err := s.canManageNursery(ctx, actor, existing.NurseryID); err != nil {
		return SourcingPost{}, err
	}
	req.Status = strings.ToUpper(strings.TrimSpace(req.Status))
	if req.Status == "" {
		req.Status = existing.Status
	}
	req.Urgency = strings.ToUpper(strings.TrimSpace(req.Urgency))
	if req.Urgency == "NORMAL" {
		req.Urgency = "FLEXIBLE"
	}
	if req.Urgency == "HIGH" {
		req.Urgency = "URGENT"
	}
	if !isAllowedPostStatus(req.Status) {
		return SourcingPost{}, ErrInvalidInput
	}
	p, err := s.repository.UpdatePost(ctx, postID, req)
	if err != nil {
		return SourcingPost{}, err
	}
	s.audit(ctx, actor, "sourcing_posts", postID, actionUpdate, req)
	return *p, nil
}

func (s *Service) DeletePost(ctx context.Context, actor ActorContext, postID int64) error {
	existing, err := s.repository.GetPost(ctx, postID)
	if err != nil {
		return err
	}
	if err := s.canManageNursery(ctx, actor, existing.NurseryID); err != nil {
		return err
	}
	if err := s.repository.DeletePost(ctx, postID); err != nil {
		return err
	}
	s.audit(ctx, actor, "sourcing_posts", postID, actionDelete, nil)
	return nil
}

// ---- Post responses ----

func (s *Service) ListResponses(ctx context.Context, actor ActorContext, postID int64) ([]PostResponse, error) {
	if !canSource(actor) {
		return nil, ErrForbidden
	}
	return s.repository.ListResponses(ctx, postID)
}

func (s *Service) CreateResponse(ctx context.Context, actor ActorContext, postID int64, req CreateResponseRequest) (PostResponse, error) {
	if err := s.canManageNursery(ctx, actor, req.ResponderNurseryID); err != nil {
		return PostResponse{}, err
	}
	if req.ResponderNurseryID <= 0 {
		return PostResponse{}, ErrInvalidInput
	}
	// Cannot respond to own post
	post, err := s.repository.GetPost(ctx, postID)
	if err != nil {
		return PostResponse{}, err
	}
	if post.NurseryID == req.ResponderNurseryID {
		return PostResponse{}, ErrInvalidInput
	}
	resp, err := s.repository.CreateResponse(ctx, postID, actor.UserID, req)
	if err != nil {
		return PostResponse{}, err
	}
	s.audit(ctx, actor, "sourcing_post_responses", resp.ID, actionInsert, req)
	return *resp, nil
}

func (s *Service) UpdateResponse(ctx context.Context, actor ActorContext, postID int64, responseID int64, req UpdateResponseRequest) (PostResponse, error) {
	// Only the post's nursery (owner/manager) may accept/decline responses
	post, err := s.repository.GetPost(ctx, postID)
	if err != nil {
		return PostResponse{}, err
	}
	if err := s.canManageNursery(ctx, actor, post.NurseryID); err != nil {
		return PostResponse{}, err
	}
	req.Status = strings.ToUpper(strings.TrimSpace(req.Status))
	if req.Status != "ACCEPTED" && req.Status != "DECLINED" {
		return PostResponse{}, ErrInvalidInput
	}
	resp, err := s.repository.UpdateResponse(ctx, responseID, req)
	if err != nil {
		return PostResponse{}, err
	}
	s.audit(ctx, actor, "sourcing_post_responses", responseID, actionUpdate, req)
	return *resp, nil
}

// ---- Helpers ----

func validatePostRequest(req CreatePostRequest) error {
	if req.NurseryID <= 0 {
		return ErrInvalidInput
	}
	if strings.TrimSpace(req.PlantName) == "" {
		return ErrInvalidInput
	}
	pt := strings.ToUpper(req.PostType)
	if pt != "NEED" && pt != "AVAILABLE" {
		return ErrInvalidInput
	}
	urgency := strings.ToUpper(req.Urgency)
	if urgency != "" && urgency != "TODAY" && urgency != "URGENT" && urgency != "FLEXIBLE" && urgency != "NORMAL" {
		return ErrInvalidInput
	}
	return nil
}

func isAllowedPostStatus(s string) bool {
	return s == "OPEN" || s == "CLOSED" || s == "EXPIRED"
}

func hasRole(actor ActorContext, role string) bool {
	for _, r := range actor.Roles {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	return false
}

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
