package localmarket

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrForbidden    = errors.New("forbidden")
	ErrInvalidInput = errors.New("invalid input")
)

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

// ── Access guards ─────────────────────────────────────────────

func canAccessMarket(actor ActorContext) bool {
	return hasRole(actor, "NURSERY_OWNER") || hasRole(actor, "MANAGER") ||
		hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN")
}

func (s *Service) actorNurseryID(ctx context.Context, actor ActorContext) (int64, error) {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return 0, fmt.Errorf("admin has no nursery context")
	}
	return s.repo.NurseryIDForUser(ctx, actor.UserID)
}

// ── Ads ───────────────────────────────────────────────────────

func (s *Service) CreateAd(ctx context.Context, actor ActorContext, req CreateAdRequest) (Ad, error) {
	if !canAccessMarket(actor) {
		return Ad{}, ErrForbidden
	}
	if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.PlantName) == "" {
		return Ad{}, fmt.Errorf("%w: title and plant_name are required", ErrInvalidInput)
	}
	if len(req.Photos) > 10 {
		return Ad{}, fmt.Errorf("%w: maximum 10 photos allowed", ErrInvalidInput)
	}
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return Ad{}, ErrForbidden
	}
	active, err := s.repo.IsNurseryActive(ctx, nurseryID)
	if err != nil || !active {
		return Ad{}, fmt.Errorf("%w: nursery must be ACTIVE to post ads", ErrForbidden)
	}
	return s.repo.CreateAd(ctx, nurseryID, actor.UserID, req)
}

func (s *Service) GetAd(ctx context.Context, actor ActorContext, id int64) (Ad, error) {
	if !canAccessMarket(actor) {
		return Ad{}, ErrForbidden
	}
	ad, err := s.repo.GetAd(ctx, id)
	if err != nil {
		return Ad{}, err
	}
	nurseryID, _ := s.actorNurseryID(ctx, actor)
	isOwner := ad.NurseryID == nurseryID
	if !isOwner && ad.Status != StatusPublished {
		return Ad{}, ErrNotFound
	}
	if !isOwner && nurseryID > 0 {
		if err := s.repo.RecordView(ctx, id, nurseryID); err == nil {
			_ = s.repo.IncrementViewCount(ctx, id)
			ad.ViewCount++
		}
		ad.IsSavedByMe, _ = s.repo.IsSaved(ctx, id, nurseryID)
	}
	return ad, nil
}

func (s *Service) BrowseAds(ctx context.Context, actor ActorContext, q AdsQuery) ([]Ad, int, error) {
	if !canAccessMarket(actor) {
		return nil, 0, ErrForbidden
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PerPage < 1 || q.PerPage > 50 {
		q.PerPage = 20
	}
	nurseryID, _ := s.actorNurseryID(ctx, actor)
	ads, total, err := s.repo.ListPublished(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	if nurseryID > 0 {
		for i := range ads {
			ads[i].IsSavedByMe, _ = s.repo.IsSaved(ctx, ads[i].ID, nurseryID)
		}
	}
	return ads, total, nil
}

func (s *Service) MyAds(ctx context.Context, actor ActorContext, q AdsQuery) ([]Ad, int, error) {
	if !canAccessMarket(actor) {
		return nil, 0, ErrForbidden
	}
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return nil, 0, ErrForbidden
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PerPage < 1 || q.PerPage > 50 {
		q.PerPage = 20
	}
	return s.repo.ListByNursery(ctx, nurseryID, q)
}

func (s *Service) UpdateAd(ctx context.Context, actor ActorContext, id int64, req UpdateAdRequest) (Ad, error) {
	if !canAccessMarket(actor) {
		return Ad{}, ErrForbidden
	}
	ad, err := s.repo.GetAd(ctx, id)
	if err != nil {
		return Ad{}, err
	}
	if err := s.requireOwner(ctx, actor, ad.NurseryID); err != nil {
		return Ad{}, err
	}
	if ad.Status == StatusArchived {
		return Ad{}, fmt.Errorf("%w: archived ads cannot be edited", ErrInvalidInput)
	}
	if req.Photos != nil && len(req.Photos) > 10 {
		return Ad{}, fmt.Errorf("%w: maximum 10 photos allowed", ErrInvalidInput)
	}
	return s.repo.UpdateAd(ctx, id, actor.UserID, req)
}

func (s *Service) PublishAd(ctx context.Context, actor ActorContext, id int64) (Ad, error) {
	return s.adTransition(ctx, actor, id, StatusPublished, []string{StatusDraft, StatusPaused}, func(ctx context.Context) error {
		now := time.Now()
		expiresAt := now.AddDate(0, 0, AdExpireDays)
		if err := s.repo.SetExpiry(ctx, id, expiresAt); err != nil {
			return err
		}
		return s.repo.SetAdStatus(ctx, id, StatusPublished, &now)
	})
}

func (s *Service) PauseAd(ctx context.Context, actor ActorContext, id int64) (Ad, error) {
	return s.adTransition(ctx, actor, id, StatusPaused, []string{StatusPublished}, func(ctx context.Context) error {
		now := time.Now()
		return s.repo.SetAdStatus(ctx, id, StatusPaused, &now)
	})
}

func (s *Service) ResumeAd(ctx context.Context, actor ActorContext, id int64) (Ad, error) {
	return s.adTransition(ctx, actor, id, StatusPublished, []string{StatusPaused}, func(ctx context.Context) error {
		now := time.Now()
		return s.repo.SetAdStatus(ctx, id, StatusPublished, &now)
	})
}

func (s *Service) RenewAd(ctx context.Context, actor ActorContext, id int64) (Ad, error) {
	return s.adTransition(ctx, actor, id, StatusPublished, []string{StatusExpired}, func(ctx context.Context) error {
		now := time.Now()
		expiresAt := now.AddDate(0, 0, AdExpireDays)
		if err := s.repo.SetExpiry(ctx, id, expiresAt); err != nil {
			return err
		}
		return s.repo.SetAdStatus(ctx, id, StatusPublished, &now)
	})
}

func (s *Service) ArchiveAd(ctx context.Context, actor ActorContext, id int64) (Ad, error) {
	return s.adTransition(ctx, actor, id, StatusArchived,
		[]string{StatusDraft, StatusPublished, StatusPaused, StatusExpired},
		func(ctx context.Context) error {
			now := time.Now()
			return s.repo.SetAdStatus(ctx, id, StatusArchived, &now)
		})
}

func (s *Service) adTransition(ctx context.Context, actor ActorContext, id int64, _ string, allowed []string, fn func(context.Context) error) (Ad, error) {
	if !canAccessMarket(actor) {
		return Ad{}, ErrForbidden
	}
	ad, err := s.repo.GetAd(ctx, id)
	if err != nil {
		return Ad{}, err
	}
	if err := s.requireOwner(ctx, actor, ad.NurseryID); err != nil {
		return Ad{}, err
	}
	if !contains(allowed, ad.Status) {
		return Ad{}, fmt.Errorf("%w: cannot transition from %s", ErrInvalidInput, ad.Status)
	}
	if err := fn(ctx); err != nil {
		return Ad{}, err
	}
	return s.repo.GetAd(ctx, id)
}

// ── Save / Bookmark ───────────────────────────────────────────

func (s *Service) ToggleSave(ctx context.Context, actor ActorContext, adID int64) (bool, error) {
	if !canAccessMarket(actor) {
		return false, ErrForbidden
	}
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return false, ErrForbidden
	}
	ad, err := s.repo.GetAd(ctx, adID)
	if err != nil {
		return false, err
	}
	if ad.NurseryID == nurseryID {
		return false, fmt.Errorf("%w: cannot save your own ad", ErrInvalidInput)
	}
	return s.repo.ToggleSave(ctx, adID, nurseryID, actor.UserID)
}

func (s *Service) SavedAds(ctx context.Context, actor ActorContext, q AdsQuery) ([]Ad, int, error) {
	if !canAccessMarket(actor) {
		return nil, 0, ErrForbidden
	}
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return nil, 0, ErrForbidden
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PerPage < 1 || q.PerPage > 50 {
		q.PerPage = 20
	}
	ads, total, err := s.repo.ListSaved(ctx, nurseryID, q)
	if err != nil {
		return nil, 0, err
	}
	for i := range ads {
		ads[i].IsSavedByMe = true
	}
	return ads, total, nil
}

// ── Reports ───────────────────────────────────────────────────

var validReasons = map[string]bool{
	"SPAM": true, "WRONG_PLANT": true, "DUPLICATE": true, "FRAUD": true, "OTHER": true,
}

func (s *Service) ReportAd(ctx context.Context, actor ActorContext, adID int64, req ReportAdRequest) error {
	if !canAccessMarket(actor) {
		return ErrForbidden
	}
	if !validReasons[req.Reason] {
		return fmt.Errorf("%w: reason must be SPAM, WRONG_PLANT, DUPLICATE, FRAUD, or OTHER", ErrInvalidInput)
	}
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return ErrForbidden
	}
	ad, err := s.repo.GetAd(ctx, adID)
	if err != nil {
		return err
	}
	if ad.NurseryID == nurseryID {
		return fmt.Errorf("%w: cannot report your own ad", ErrInvalidInput)
	}
	return s.repo.CreateReport(ctx, adID, actor.UserID, nurseryID, req.Reason, req.Notes)
}

// ── Enquiries ─────────────────────────────────────────────────

func (s *Service) SendEnquiry(ctx context.Context, actor ActorContext, adID int64, req CreateEnquiryRequest) (Enquiry, error) {
	if !canAccessMarket(actor) {
		return Enquiry{}, ErrForbidden
	}
	if strings.TrimSpace(req.Message) == "" {
		return Enquiry{}, fmt.Errorf("%w: message is required", ErrInvalidInput)
	}
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return Enquiry{}, ErrForbidden
	}
	ad, err := s.repo.GetAd(ctx, adID)
	if err != nil {
		return Enquiry{}, err
	}
	if ad.Status != StatusPublished {
		return Enquiry{}, fmt.Errorf("%w: ad is not available", ErrInvalidInput)
	}
	if ad.NurseryID == nurseryID {
		return Enquiry{}, fmt.Errorf("%w: cannot enquire about your own ad", ErrInvalidInput)
	}
	already, err := s.repo.HasEnquiry(ctx, adID, nurseryID)
	if err != nil {
		return Enquiry{}, err
	}
	if already {
		return Enquiry{}, fmt.Errorf("%w: you have already sent an enquiry for this ad", ErrInvalidInput)
	}
	return s.repo.CreateEnquiry(ctx, adID, ad.NurseryID, nurseryID, actor.UserID, req)
}

func (s *Service) GetEnquiry(ctx context.Context, actor ActorContext, id int64) (Enquiry, error) {
	if !canAccessMarket(actor) {
		return Enquiry{}, ErrForbidden
	}
	e, err := s.repo.GetEnquiry(ctx, id)
	if err != nil {
		return Enquiry{}, err
	}
	nurseryID, _ := s.actorNurseryID(ctx, actor)
	if !s.canAccessEnquiry(actor, e, nurseryID) {
		return Enquiry{}, ErrForbidden
	}
	if nurseryID == e.AdNurseryID {
		_ = s.repo.MarkEnquiryViewed(ctx, id)
	}
	msgs, _ := s.repo.GetMessages(ctx, id)
	e.Messages = msgs
	return e, nil
}

func (s *Service) ListEnquiries(ctx context.Context, actor ActorContext, q EnquiriesQuery) ([]Enquiry, int, error) {
	if !canAccessMarket(actor) {
		return nil, 0, ErrForbidden
	}
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return nil, 0, ErrForbidden
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PerPage < 1 || q.PerPage > 50 {
		q.PerPage = 20
	}
	return s.repo.ListEnquiries(ctx, nurseryID, q)
}

func (s *Service) ReplyToEnquiry(ctx context.Context, actor ActorContext, id int64, req ReplyEnquiryRequest) (Message, error) {
	if !canAccessMarket(actor) {
		return Message{}, ErrForbidden
	}
	if strings.TrimSpace(req.Body) == "" {
		return Message{}, fmt.Errorf("%w: body is required", ErrInvalidInput)
	}
	e, err := s.repo.GetEnquiry(ctx, id)
	if err != nil {
		return Message{}, err
	}
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return Message{}, ErrForbidden
	}
	if !s.canAccessEnquiry(actor, e, nurseryID) {
		return Message{}, ErrForbidden
	}
	if e.Status == EnquiryClosed || e.Status == EnquiryCancelled {
		return Message{}, fmt.Errorf("%w: enquiry is %s", ErrInvalidInput, e.Status)
	}
	return s.repo.AddMessage(ctx, id, actor.UserID, nurseryID, req.Body)
}

func (s *Service) CloseEnquiry(ctx context.Context, actor ActorContext, id int64) (Enquiry, error) {
	return s.updateEnquiryStatus(ctx, actor, id, EnquiryClosed)
}

func (s *Service) CancelEnquiry(ctx context.Context, actor ActorContext, id int64) (Enquiry, error) {
	e, err := s.repo.GetEnquiry(ctx, id)
	if err != nil {
		return Enquiry{}, err
	}
	nurseryID, _ := s.actorNurseryID(ctx, actor)
	if nurseryID != e.EnquiringNurseryID {
		return Enquiry{}, ErrForbidden
	}
	return s.updateEnquiryStatus(ctx, actor, id, EnquiryCancelled)
}

func (s *Service) LinkQuotation(ctx context.Context, actor ActorContext, id int64, req LinkQuotationRequest) (Enquiry, error) {
	if !canAccessMarket(actor) {
		return Enquiry{}, ErrForbidden
	}
	e, err := s.repo.GetEnquiry(ctx, id)
	if err != nil {
		return Enquiry{}, err
	}
	nurseryID, _ := s.actorNurseryID(ctx, actor)
	if nurseryID != e.AdNurseryID {
		return Enquiry{}, ErrForbidden
	}
	if err := s.repo.LinkQuotation(ctx, id, req.QuotationID); err != nil {
		return Enquiry{}, err
	}
	return s.repo.GetEnquiry(ctx, id)
}

func (s *Service) updateEnquiryStatus(ctx context.Context, actor ActorContext, id int64, newStatus string) (Enquiry, error) {
	if !canAccessMarket(actor) {
		return Enquiry{}, ErrForbidden
	}
	e, err := s.repo.GetEnquiry(ctx, id)
	if err != nil {
		return Enquiry{}, err
	}
	nurseryID, _ := s.actorNurseryID(ctx, actor)
	if !s.canAccessEnquiry(actor, e, nurseryID) {
		return Enquiry{}, ErrForbidden
	}
	if err := s.repo.SetEnquiryStatus(ctx, id, newStatus); err != nil {
		return Enquiry{}, err
	}
	return s.repo.GetEnquiry(ctx, id)
}

// ── Helpers ───────────────────────────────────────────────────

func (s *Service) requireOwner(ctx context.Context, actor ActorContext, nurseryID int64) error {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return nil
	}
	ok, err := s.repo.IsNurseryMember(ctx, nurseryID, actor.UserID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}

func (s *Service) canAccessEnquiry(actor ActorContext, e Enquiry, nurseryID int64) bool {
	if hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN") {
		return true
	}
	return nurseryID == e.AdNurseryID || nurseryID == e.EnquiringNurseryID
}

func hasRole(actor ActorContext, role string) bool {
	for _, r := range actor.Roles {
		if r == role {
			return true
		}
	}
	return false
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
