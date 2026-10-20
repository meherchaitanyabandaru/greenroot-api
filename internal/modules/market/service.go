package market

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
	"github.com/redis/go-redis/v9"
)

var (
	ErrForbidden    = apperrs.ErrForbidden
	ErrInvalidInput = apperrs.ErrInvalidInput
	ErrConflict     = apperrs.ErrConflict
)

// validateAdType defaults the type to SELL and enforces what a WANT is: a
// catalog plant, a size and an optional quantity and date, never a price or
// photos (sellers answer with an offer in the chat).
func validateAdType(req *CreateAdRequest) error {
	req.AdType = strings.ToUpper(strings.TrimSpace(req.AdType))
	if req.AdType == "" {
		req.AdType = AdTypeSell
	}
	switch req.AdType {
	case AdTypeSell:
		if req.NeededBy != nil {
			return fmt.Errorf("%w: needed_by is only for wanted posts", ErrInvalidInput)
		}
		return nil
	case AdTypeWant:
		if req.PlantID == nil || *req.PlantID <= 0 {
			return fmt.Errorf("%w: pick the plant you want from the catalog", ErrInvalidInput)
		}
		if req.PricePerUnit != nil || req.PriceUnit != nil {
			return fmt.Errorf("%w: a wanted post has no price; sellers offer one in the chat", ErrInvalidInput)
		}
		if len(req.Photos) > 0 {
			return fmt.Errorf("%w: a wanted post has no photos", ErrInvalidInput)
		}
		if req.NeededBy != nil {
			if *req.NeededBy == "" {
				req.NeededBy = nil
			} else if err := validateNeededBy(*req.NeededBy); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: ad_type must be SELL or WANT", ErrInvalidInput)
	}
}

func validateNeededBy(v string) error {
	d, err := time.Parse("2006-01-02", v)
	if err != nil {
		return fmt.Errorf("%w: needed_by must be a date like 2026-11-30", ErrInvalidInput)
	}
	if d.Before(time.Now().Truncate(24*time.Hour).AddDate(0, 0, -1)) {
		return fmt.Errorf("%w: needed_by is in the past", ErrInvalidInput)
	}
	return nil
}

func (s *Service) SellerProfile(ctx context.Context, actor ActorContext, nurseryID int64) (SellerProfile, error) {
	if !canAccessMarket(actor) {
		return SellerProfile{}, ErrForbidden
	}
	p, err := s.repo.SellerProfile(ctx, nurseryID)
	if err != nil {
		return SellerProfile{}, err
	}
	viewer, _ := s.actorNurseryID(ctx, actor)
	for i := range p.Ads {
		p.Ads[i] = enrichAd(p.Ads[i], viewer)
	}
	return p, nil
}

type Service struct {
	repo  Repository
	redis redis.Cmdable
}

func NewService(repo Repository, redisClients ...redis.Cmdable) *Service {
	var rdb redis.Cmdable
	if len(redisClients) > 0 {
		rdb = redisClients[0]
	}
	return &Service{repo: repo, redis: rdb}
}

// ── Access guards ─────────────────────────────────────────────

func canAccessMarket(actor ActorContext) bool {
	return actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER") ||
		actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN")
}

func (s *Service) actorNurseryID(ctx context.Context, actor ActorContext) (int64, error) {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
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
	if err := validateAdType(&req); err != nil {
		return Ad{}, err
	}
	if len(req.Photos) > 10 {
		return Ad{}, fmt.Errorf("%w: maximum 10 photos allowed", ErrInvalidInput)
	}
	req = markConfirmedPickup(actor, req)
	if err := validatePickup(req.PickupLatitude, req.PickupLongitude, req.PickupGPSAccuracyM, req.PickupLocationSource); err != nil {
		return Ad{}, err
	}
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return Ad{}, ErrForbidden
	}
	active, err := s.repo.IsNurseryActive(ctx, nurseryID)
	if err != nil || !active {
		return Ad{}, fmt.Errorf("%w: nursery must be ACTIVE to post ads", ErrForbidden)
	}
	if err := s.checkPostingAllowed(ctx, nurseryID); err != nil {
		return Ad{}, err
	}
	// No pickup given: use the nursery's own address, so the ad shows where
	// to collect and (when the address has coordinates) "Near me" finds it.
	if req.PickupAddress == nil && req.PickupLatitude == nil {
		addr, lat, lon, err := s.repo.NurseryPickup(ctx, nurseryID)
		if err != nil {
			return Ad{}, err
		}
		if addr != nil || lat != nil {
			src := "nursery_default"
			req.PickupAddress, req.PickupLatitude, req.PickupLongitude, req.PickupLocationSource = addr, lat, lon, &src
		}
	}
	ad, err := s.repo.CreateAd(ctx, nurseryID, actor.UserID, req)
	if err != nil {
		return Ad{}, err
	}
	s.bumpMarketSearchCache(ctx, actor.UserID)
	return enrichAd(ad, nurseryID), nil
}

// bumpMarketSearchCache invalidates the acting user's previously-cached
// Global Search "Market" results (see BrowseAds's cache-aside path).
func (s *Service) bumpMarketSearchCache(ctx context.Context, userID int64) {
	redisutil.BumpSearchGeneration(ctx, s.redis, nil, "market_ads", userID)
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
			s.incrementAdCounter(ctx, redisutil.KeyAdViews+strconv.FormatInt(id, 10), 1, func() {
				_ = s.repo.FlushAdCounters(ctx, map[int64]int64{id: 1}, nil)
			})
		}
		ad.IsSavedByMe, _ = s.repo.IsSaved(ctx, id, nurseryID)
		if enquiryID, err := s.repo.EnquiryIDFor(ctx, id, nurseryID); err == nil && enquiryID > 0 {
			ad.MyEnquiryID = &enquiryID
		}
	}
	s.applyAdCounterDeltas(ctx, []Ad{ad}, func(updated Ad) { ad = updated })
	return enrichAd(ad, nurseryID), nil
}

// cachedAdsList is the Global Search "Market" category's cache-aside
// payload. Ads are actor-personalized (IsSavedByMe), so unlike plants this
// is always cached per-user (see BrowseAds), never shared across actors.
type cachedAdsList struct {
	Ads   []Ad `json:"ads"`
	Total int  `json:"total"`
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

	if q.Search == "" {
		return s.browseAds(ctx, actor, q)
	}

	// Global Search's "Market" category (2-minute TTL). Cache key includes
	// actor.UserID -- IsSavedByMe below is actor-personalized, so this must
	// never be shared across users the way plants' cache is.
	const module = "market_ads"
	redisutil.RecordSearchTerm(ctx, s.redis, nil, module, q.Search)
	gen := redisutil.SearchGeneration(ctx, s.redis, nil, module, actor.UserID)
	key := redisutil.SearchCacheKey(module, actor.UserID, gen,
		q.Type, q.Search, q.Sort, q.Category, fmt.Sprint(q.MinPrice), fmt.Sprint(q.MaxPrice),
		fmt.Sprint(q.Page), fmt.Sprint(q.PerPage))

	if cached, ok := redisutil.GetCachedSearch[cachedAdsList](ctx, s.redis, nil, module, q.Search, key); ok {
		return cached.Ads, cached.Total, nil
	}

	start := time.Now()
	ads, total, err := s.browseAds(ctx, actor, q)
	if err != nil {
		return nil, 0, err
	}
	redisutil.SetCachedSearch(ctx, s.redis, nil, module, q.Search, key,
		cachedAdsList{Ads: ads, Total: total}, redisutil.SearchTTLMarketAds, time.Since(start))
	return ads, total, nil
}

func (s *Service) browseAds(ctx context.Context, actor ActorContext, q AdsQuery) ([]Ad, int, error) {
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
	s.applyAdCounterDeltas(ctx, ads, func(updated Ad) {
		for i := range ads {
			if ads[i].ID == updated.ID {
				ads[i] = updated
				break
			}
		}
	})
	for i := range ads {
		ads[i] = enrichAd(ads[i], nurseryID)
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
	ads, total, err := s.repo.ListByNursery(ctx, nurseryID, q)
	if err != nil {
		return nil, 0, err
	}
	s.applyAdCounterDeltas(ctx, ads, func(updated Ad) {
		for i := range ads {
			if ads[i].ID == updated.ID {
				ads[i] = updated
				break
			}
		}
	})
	for i := range ads {
		ads[i] = enrichAd(ads[i], nurseryID)
	}
	return ads, total, nil
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
	if ad.AdType == AdTypeWant && (req.PricePerUnit != nil || req.PriceUnit != nil || len(req.Photos) > 0) {
		return Ad{}, fmt.Errorf("%w: a wanted post has no price or photos; sellers offer a price in the chat", ErrInvalidInput)
	}
	if req.NeededBy != nil && ad.AdType != AdTypeWant {
		return Ad{}, fmt.Errorf("%w: needed_by is only for wanted posts", ErrInvalidInput)
	}
	if req.NeededBy != nil && *req.NeededBy != "" {
		if err := validateNeededBy(*req.NeededBy); err != nil {
			return Ad{}, err
		}
	}
	switch ad.Status {
	case StatusArchived, StatusRemoved:
		return Ad{}, fmt.Errorf("%w: this ad is closed and cannot be edited", ErrInvalidInput)
	case StatusSuspended:
		return Ad{}, fmt.Errorf("%w: this ad is under review and cannot be edited", ErrConflict)
	}
	if req.Photos != nil && len(req.Photos) > 10 {
		return Ad{}, fmt.Errorf("%w: maximum 10 photos allowed", ErrInvalidInput)
	}
	req = markConfirmedPickupUpdate(actor, req)
	if err := validatePickup(req.PickupLatitude, req.PickupLongitude, req.PickupGPSAccuracyM, req.PickupLocationSource); err != nil {
		return Ad{}, err
	}
	updated, err := s.repo.UpdateAd(ctx, id, actor.UserID, req)
	if err != nil {
		return Ad{}, err
	}
	nurseryID, _ := s.actorNurseryID(ctx, actor)
	s.bumpMarketSearchCache(ctx, actor.UserID)
	return enrichAd(updated, nurseryID), nil
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

// DeleteAd soft-deletes the owner's ad. Only drafts and live/paused ads nobody
// has chatted on can go; anything with history is archived instead, so the
// other side's chat and any report stay intact.
func (s *Service) DeleteAd(ctx context.Context, actor ActorContext, id int64) error {
	if !canAccessMarket(actor) {
		return ErrForbidden
	}
	ad, err := s.repo.GetAd(ctx, id)
	if err != nil {
		return err
	}
	if err := s.requireOwner(ctx, actor, ad.NurseryID); err != nil {
		return err
	}
	if !canDeleteAd(ad) {
		return fmt.Errorf("%w: only drafts and ads nobody has chatted on can be deleted; archive this ad instead", ErrConflict)
	}
	ok, err := s.repo.SoftDeleteAd(ctx, id, actor.UserID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: this ad now has a chat or a report; archive it instead", ErrConflict)
	}
	s.bumpMarketSearchCache(ctx, actor.UserID)
	return nil
}

func canDeleteAd(ad Ad) bool {
	return ad.Status == StatusDraft ||
		((ad.Status == StatusPublished || ad.Status == StatusPaused) && ad.EnquiryCount == 0)
}

func (s *Service) adTransition(ctx context.Context, actor ActorContext, id int64, target string, allowed []string, fn func(context.Context) error) (Ad, error) {
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
	if target == StatusPublished {
		if err := s.checkPostingAllowed(ctx, ad.NurseryID); err != nil {
			return Ad{}, err
		}
	}
	if err := fn(ctx); err != nil {
		return Ad{}, err
	}
	updated, err := s.repo.GetAd(ctx, id)
	if err != nil {
		return Ad{}, err
	}
	nurseryID, _ := s.actorNurseryID(ctx, actor)
	// Publish/Pause/Resume/Archive all change whether this ad shows up in
	// BrowseAds' published-only results -- an "update" for search purposes.
	s.bumpMarketSearchCache(ctx, actor.UserID)
	return enrichAd(updated, nurseryID), nil
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
	saved, err := s.repo.ToggleSave(ctx, adID, nurseryID, actor.UserID)
	if err != nil {
		return false, err
	}
	delta := int64(1)
	if !saved {
		delta = -1
	}
	s.incrementAdCounter(ctx, redisutil.KeyAdSaves+strconv.FormatInt(adID, 10), delta, func() {
		_ = s.repo.FlushAdCounters(ctx, nil, map[int64]int64{adID: delta})
	})
	return saved, nil
}

func (s *Service) incrementAdCounter(ctx context.Context, key string, delta int64, fallback func()) {
	if s.redis == nil {
		if fallback != nil {
			fallback()
		}
		return
	}
	var err error
	if delta == 1 {
		err = s.redis.Incr(ctx, key).Err()
	} else {
		err = s.redis.IncrBy(ctx, key, delta).Err()
	}
	if err != nil {
		slog.Warn("redis market ad counter increment failed", "key", key, "delta", delta, "error", err)
		if fallback != nil {
			fallback()
		}
	}
}

func (s *Service) applyAdCounterDeltas(ctx context.Context, ads []Ad, replace func(Ad)) {
	if s.redis == nil || len(ads) == 0 {
		return
	}
	keys := make([]string, 0, len(ads)*2)
	for _, ad := range ads {
		keys = append(keys,
			redisutil.KeyAdViews+strconv.FormatInt(ad.ID, 10),
			redisutil.KeyAdSaves+strconv.FormatInt(ad.ID, 10),
		)
	}
	values, err := s.redis.MGet(ctx, keys...).Result()
	if err != nil {
		slog.Warn("redis market ad counter read failed", "error", err)
		return
	}
	for i, ad := range ads {
		viewDelta := int64Value(values[i*2])
		saveDelta := int64Value(values[i*2+1])
		if viewDelta == 0 && saveDelta == 0 {
			continue
		}
		ad.ViewCount += int(viewDelta)
		ad.SaveCount += int(saveDelta)
		if ad.SaveCount < 0 {
			ad.SaveCount = 0
		}
		replace(ad)
	}
}

func int64Value(value any) int64 {
	if value == nil {
		return 0
	}
	parsed, err := strconv.ParseInt(fmt.Sprint(value), 10, 64)
	if err != nil {
		return 0
	}
	return parsed
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
	s.applyAdCounterDeltas(ctx, ads, func(updated Ad) {
		for i := range ads {
			if ads[i].ID == updated.ID {
				ads[i] = updated
				break
			}
		}
	})
	for i := range ads {
		ads[i] = enrichAd(ads[i], nurseryID)
	}
	return ads, total, nil
}

// ── Reports ───────────────────────────────────────────────────

var validReasons = map[string]bool{
	"SPAM": true, "WRONG_PLANT": true, "DUPLICATE": true, "FRAUD": true, "OTHER": true,
	"EXPLICIT_CONTENT": true, "SCAM": true, "PROHIBITED": true,
}

// seriousReasons hide the ad on the first report, before any admin looks:
// leaving nude images or banned items up while waiting costs more than a
// wrongly hidden ad, which an admin restores.
var seriousReasons = map[string]string{
	"EXPLICIT_CONTENT": "nude or sexual images",
	"PROHIBITED":       "a banned or illegal item",
}

func (s *Service) ReportAd(ctx context.Context, actor ActorContext, adID int64, req ReportAdRequest) error {
	if !canAccessMarket(actor) {
		return ErrForbidden
	}
	if !validReasons[req.Reason] {
		return fmt.Errorf("%w: reason must be SPAM, WRONG_PLANT, DUPLICATE, FRAUD, SCAM, EXPLICIT_CONTENT, PROHIBITED or OTHER", ErrInvalidInput)
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
	if err := s.repo.CreateReport(ctx, adID, actor.UserID, nurseryID, req.Reason, req.Notes); err != nil {
		return err
	}
	if what, serious := seriousReasons[req.Reason]; serious && (ad.Status == StatusPublished || ad.Status == StatusPaused) {
		note := "Hidden after a report of " + what + ". GreenRoot is reviewing it."
		if err := s.repo.ModerateAd(ctx, adID, StatusSuspended, &note); err != nil {
			return err
		}
		s.bumpMarketSearchCache(ctx, actor.UserID)
	}
	return nil
}

// checkPostingAllowed refuses new or re-published ads while the nursery is
// blocked for repeated rule breaks.
func (s *Service) checkPostingAllowed(ctx context.Context, nurseryID int64) error {
	until, err := s.repo.PostingBlockedUntil(ctx, nurseryID)
	if err != nil {
		return err
	}
	if until != nil {
		return fmt.Errorf("%w: your nursery cannot post market ads until %s because ads broke the market rules",
			ErrForbidden, until.Format("2 Jan 2006"))
	}
	return nil
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
	existing, err := s.repo.EnquiryIDFor(ctx, adID, nurseryID)
	if err != nil {
		return Enquiry{}, err
	}
	if existing > 0 {
		return Enquiry{}, fmt.Errorf("%w: you have already sent an enquiry for this ad", ErrInvalidInput)
	}
	enquiry, err := s.repo.CreateEnquiry(ctx, adID, ad.NurseryID, nurseryID, actor.UserID, req)
	if err != nil {
		return Enquiry{}, err
	}
	return enrichEnquiry(enquiry, nurseryID), nil
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
	if nurseryID == e.AdNurseryID || nurseryID == e.EnquiringNurseryID {
		_ = s.repo.MarkChatRead(ctx, id, nurseryID)
	}
	msgs, _ := s.repo.GetMessages(ctx, id)
	e.Messages = msgs
	return enrichEnquiry(e, nurseryID), nil
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
	enquiries, total, err := s.repo.ListEnquiries(ctx, nurseryID, q)
	if err != nil {
		return nil, 0, err
	}
	if err := s.repo.ChatSummaries(ctx, enquiries, nurseryID); err != nil {
		return nil, 0, err
	}
	for i := range enquiries {
		enquiries[i] = enrichEnquiry(enquiries[i], nurseryID)
	}
	return enquiries, total, nil
}

// UnreadChats is the viewer's unread message count across open chats, for
// the app's Chats badge. Admins have no nursery and get 0.
func (s *Service) UnreadChats(ctx context.Context, actor ActorContext) int {
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return 0
	}
	n, _ := s.repo.UnreadTotal(ctx, nurseryID)
	return n
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
	if reason := readOnlyReason(e); reason != "" {
		return Message{}, fmt.Errorf("%w: %s", ErrConflict, reason)
	}
	msg, err := s.repo.AddMessage(ctx, id, actor.UserID, nurseryID, req.Body)
	msg.IsMine = true
	return msg, err
}

// MakeOffer posts a price (and optional quantity) for the other nursery to
// accept or decline. Either side can offer, so the seller counters by making
// an offer back; a new offer withdraws the chat's previous pending one.
func (s *Service) MakeOffer(ctx context.Context, actor ActorContext, id int64, req MakeOfferRequest) (Message, error) {
	if req.PricePerUnit <= 0 || req.PricePerUnit > 10_000_000 {
		return Message{}, fmt.Errorf("%w: offer price must be more than zero", ErrInvalidInput)
	}
	if req.Quantity != nil && *req.Quantity <= 0 {
		return Message{}, fmt.Errorf("%w: quantity must be more than zero", ErrInvalidInput)
	}
	e, nurseryID, err := s.writableEnquiry(ctx, actor, id)
	if err != nil {
		return Message{}, err
	}
	if e.Status == EnquiryDealAgreed {
		return Message{}, fmt.Errorf("%w: a deal is already agreed in this chat", ErrConflict)
	}
	if req.Quantity != nil && e.AdQuantity != nil && *req.Quantity > *e.AdQuantity {
		return Message{}, fmt.Errorf("%w: the ad has only %d available", ErrInvalidInput, *e.AdQuantity)
	}
	msg, err := s.repo.AddOffer(ctx, id, actor.UserID, nurseryID, req, "Offered "+dealText(req.PricePerUnit, req.Quantity, e.AdPriceUnit))
	msg.IsMine = true
	return msg, err
}

func (s *Service) AcceptOffer(ctx context.Context, actor ActorContext, id, messageID int64) (Enquiry, error) {
	return s.respondToOffer(ctx, actor, id, messageID, true)
}

func (s *Service) DeclineOffer(ctx context.Context, actor ActorContext, id, messageID int64) (Enquiry, error) {
	return s.respondToOffer(ctx, actor, id, messageID, false)
}

func (s *Service) respondToOffer(ctx context.Context, actor ActorContext, id, messageID int64, accept bool) (Enquiry, error) {
	e, nurseryID, err := s.writableEnquiry(ctx, actor, id)
	if err != nil {
		return Enquiry{}, err
	}
	dealBody := func(price float64, qty *int) string {
		return "Deal agreed: " + dealText(price, qty, e.AdPriceUnit)
	}
	if err := s.repo.RespondToOffer(ctx, id, messageID, actor.UserID, nurseryID, accept, dealBody); err != nil {
		return Enquiry{}, err
	}
	return s.GetEnquiry(ctx, actor, id)
}

// writableEnquiry loads a chat the actor's nursery is part of and refuses it
// when nobody can write to it any more.
func (s *Service) writableEnquiry(ctx context.Context, actor ActorContext, id int64) (Enquiry, int64, error) {
	if !canAccessMarket(actor) {
		return Enquiry{}, 0, ErrForbidden
	}
	nurseryID, err := s.actorNurseryID(ctx, actor)
	if err != nil {
		return Enquiry{}, 0, ErrForbidden
	}
	e, err := s.repo.GetEnquiry(ctx, id)
	if err != nil {
		return Enquiry{}, 0, err
	}
	if nurseryID != e.AdNurseryID && nurseryID != e.EnquiringNurseryID {
		return Enquiry{}, 0, ErrForbidden
	}
	if reason := readOnlyReason(e); reason != "" {
		return Enquiry{}, 0, fmt.Errorf("%w: %s", ErrConflict, reason)
	}
	return e, nurseryID, nil
}

// readOnlyReason is empty while both nurseries can still write: the chat is
// open and its ad is live. Pausing, expiring or archiving the ad freezes it.
func readOnlyReason(e Enquiry) string {
	switch e.Status {
	case EnquiryClosed:
		return "the seller closed this chat"
	case EnquiryCancelled:
		return "the buyer cancelled this enquiry"
	}
	if e.AdStatus != StatusPublished {
		return "this ad is no longer active"
	}
	return ""
}

// dealText renders "₹120 per plant × 50"; the unit falls back to "each".
func dealText(price float64, qty *int, unit *string) string {
	per := "each"
	if unit != nil && strings.TrimSpace(*unit) != "" {
		per = strings.ToLower(strings.ReplaceAll(*unit, "_", " "))
	}
	text := "₹" + strconv.FormatFloat(price, 'f', -1, 64) + " " + per
	if qty != nil {
		text += fmt.Sprintf(" × %d", *qty)
	}
	return text
}

// CloseEnquiry lets the seller end a chat, e.g. after the pickup.
func (s *Service) CloseEnquiry(ctx context.Context, actor ActorContext, id int64) (Enquiry, error) {
	return s.endEnquiry(ctx, actor, id, EnquiryClosed)
}

// CancelEnquiry lets the enquiring nursery withdraw.
func (s *Service) CancelEnquiry(ctx context.Context, actor ActorContext, id int64) (Enquiry, error) {
	return s.endEnquiry(ctx, actor, id, EnquiryCancelled)
}

func (s *Service) endEnquiry(ctx context.Context, actor ActorContext, id int64, newStatus string) (Enquiry, error) {
	if !canAccessMarket(actor) {
		return Enquiry{}, ErrForbidden
	}
	e, err := s.repo.GetEnquiry(ctx, id)
	if err != nil {
		return Enquiry{}, err
	}
	nurseryID, _ := s.actorNurseryID(ctx, actor)
	side := e.AdNurseryID // only the seller closes
	if newStatus == EnquiryCancelled {
		side = e.EnquiringNurseryID // only the buyer cancels
	}
	if nurseryID == 0 || nurseryID != side {
		return Enquiry{}, ErrForbidden
	}
	if e.Status == EnquiryClosed || e.Status == EnquiryCancelled {
		return Enquiry{}, fmt.Errorf("%w: this chat has already ended", ErrConflict)
	}
	if err := s.repo.SetEnquiryStatus(ctx, id, newStatus); err != nil {
		return Enquiry{}, err
	}
	return s.GetEnquiry(ctx, actor, id)
}

// ── Helpers ───────────────────────────────────────────────────

func (s *Service) requireOwner(ctx context.Context, actor ActorContext, nurseryID int64) error {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
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
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return true
	}
	return nurseryID == e.AdNurseryID || nurseryID == e.EnquiringNurseryID
}

func enrichAd(ad Ad, actorNurseryID int64) Ad {
	status := strings.ToUpper(strings.TrimSpace(ad.Status))
	isOwner := actorNurseryID > 0 && ad.NurseryID == actorNurseryID
	isLive := status == StatusPublished
	isExpired := status == StatusExpired || (ad.ExpiresAt != nil && time.Now().After(*ad.ExpiresAt))

	ad.Lifecycle = marketPtr(lifecycle.MarketAd(status))
	ad.Summary = &AdSummary{
		IsOwner:       isOwner,
		IsLive:        isLive,
		IsExpired:     isExpired,
		DaysRemaining: daysRemaining(ad.ExpiresAt),
	}
	ad.Capabilities = &AdCapabilities{
		CanEdit:    isOwner && !contains([]string{StatusArchived, StatusSuspended, StatusRemoved}, status),
		CanPublish: isOwner && (status == StatusDraft || status == StatusPaused),
		CanPause:   isOwner && status == StatusPublished,
		CanResume:  isOwner && status == StatusPaused,
		CanRenew:   isOwner && status == StatusExpired,
		CanArchive: isOwner && !contains([]string{StatusArchived, StatusSuspended, StatusRemoved}, status),
		CanDelete:  isOwner && canDeleteAd(ad),
		CanSave:    !isOwner && status == StatusPublished,
		CanEnquire: !isOwner && status == StatusPublished && ad.MyEnquiryID == nil,
		CanReport:  !isOwner && status == StatusPublished,
	}
	return ad
}

func enrichEnquiry(enquiry Enquiry, actorNurseryID int64) Enquiry {
	status := strings.ToUpper(strings.TrimSpace(enquiry.Status))
	isBuyer := actorNurseryID > 0 && enquiry.EnquiringNurseryID == actorNurseryID
	isSeller := actorNurseryID > 0 && enquiry.AdNurseryID == actorNurseryID
	isOpen := status != EnquiryClosed && status != EnquiryCancelled
	readOnly := readOnlyReason(enquiry)
	canWrite := readOnly == "" && (isBuyer || isSeller)

	enquiry.Lifecycle = marketPtr(lifecycle.MarketEnquiry(status))
	enquiry.Summary = &EnquirySummary{
		IsBuyer:  isBuyer,
		IsSeller: isSeller,
		IsOpen:   isOpen,
	}
	enquiry.Capabilities = &EnquiryCapabilities{
		CanReply:       canWrite,
		CanOffer:       canWrite && status != EnquiryDealAgreed,
		CanClose:       isOpen && isSeller,
		CanCancel:      isOpen && isBuyer,
		ReadOnlyReason: readOnly,
	}
	for i := range enquiry.Messages {
		m := &enquiry.Messages[i]
		m.IsMine = actorNurseryID > 0 && m.SentByNurseryID == actorNurseryID
		m.CanRespond = canWrite && !m.IsMine && status != EnquiryDealAgreed &&
			m.OfferStatus != nil && *m.OfferStatus == OfferPending
	}
	return enquiry
}

func daysRemaining(expiresAt *time.Time) *int {
	if expiresAt == nil {
		return nil
	}
	days := int(expiresAt.Sub(time.Now()).Hours() / 24)
	if expiresAt.After(time.Now()) && days == 0 {
		days = 1
	}
	return &days
}

func marketPtr[T any](value T) *T {
	return &value
}

func markConfirmedPickup(actor ActorContext, req CreateAdRequest) CreateAdRequest {
	if req.PickupLocationSource != nil && strings.TrimSpace(*req.PickupLocationSource) != "" {
		req.PickupConfirmedBy = &actor.UserID
		now := time.Now()
		req.PickupConfirmedAt = &now
	}
	return req
}

func markConfirmedPickupUpdate(actor ActorContext, req UpdateAdRequest) UpdateAdRequest {
	if req.PickupLocationSource != nil && strings.TrimSpace(*req.PickupLocationSource) != "" {
		req.PickupConfirmedBy = &actor.UserID
		now := time.Now()
		req.PickupConfirmedAt = &now
	}
	return req
}

func validatePickup(latitude *float64, longitude *float64, accuracy *float64, source *string) error {
	if (latitude == nil) != (longitude == nil) {
		return fmt.Errorf("%w: pickup latitude and longitude must be provided together", ErrInvalidInput)
	}
	if latitude != nil && (*latitude < -90 || *latitude > 90) {
		return fmt.Errorf("%w: invalid pickup latitude", ErrInvalidInput)
	}
	if longitude != nil && (*longitude < -180 || *longitude > 180) {
		return fmt.Errorf("%w: invalid pickup longitude", ErrInvalidInput)
	}
	if accuracy != nil && *accuracy < 0 {
		return fmt.Errorf("%w: invalid pickup accuracy", ErrInvalidInput)
	}
	if source != nil && !isAllowedLocationSource(*source) {
		return fmt.Errorf("%w: invalid pickup location source", ErrInvalidInput)
	}
	return nil
}

func isAllowedLocationSource(value string) bool {
	switch strings.TrimSpace(value) {
	case "", "gps_confirmed", "nursery_default", "map_selected", "address_search", "admin_updated":
		return true
	default:
		return false
	}
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
