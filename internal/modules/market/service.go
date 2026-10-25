package market

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/redis/go-redis/v9"
)

var (
	ErrForbidden    = apperrs.ErrForbidden
	ErrInvalidInput = apperrs.ErrInvalidInput
	ErrConflict     = apperrs.ErrConflict
	// ErrLocationRequired: the viewer sent no position and their nursery has
	// no coordinates, so "nearby" cannot be worked out.
	ErrLocationRequired = errors.New("location required")
)

// The market is local: everyone sees ads around them, never across the
// country. 60 km unless the viewer widens it, and never past 100 km.
const (
	DefaultRadiusKM = 60.0
	MaxRadiusKM     = 100.0
)

// nearbyArea fills the viewer's position (the phone's, else their nursery's
// address) and clamps the radius. Admins browse everything.
func (s *Service) nearbyArea(ctx context.Context, actor ActorContext, q *AdsQuery) error {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") {
		return nil
	}
	if q.NearLat == nil || q.NearLon == nil {
		nurseryID, err := s.actorNurseryID(ctx, actor)
		if err != nil {
			return ErrForbidden
		}
		_, lat, lon, err := s.repo.NurseryPickup(ctx, nurseryID)
		if err != nil {
			return err
		}
		if lat == nil || lon == nil {
			return ErrLocationRequired
		}
		q.NearLat, q.NearLon = lat, lon
	}
	if *q.NearLat < -90 || *q.NearLat > 90 || *q.NearLon < -180 || *q.NearLon > 180 {
		return fmt.Errorf("%w: invalid near_lat / near_lon", ErrInvalidInput)
	}
	radius := DefaultRadiusKM
	if q.RadiusKM != nil && *q.RadiusKM > 0 {
		radius = min(*q.RadiusKM, MaxRadiusKM)
	}
	q.RadiusKM = &radius
	return nil
}

// checkSize allows only catalog sizes (Seed, Sapling, Small, ...): a bare
// "44" tells buyers nothing, with no unit. Empty means no size.
func (s *Service) checkSize(ctx context.Context, size *string) error {
	if size == nil || strings.TrimSpace(*size) == "" {
		return nil
	}
	ok, err := s.repo.IsCatalogSize(ctx, strings.TrimSpace(*size))
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: pick a size from the list (Seed, Sapling, Small, Medium, Large, Extra Large)", ErrInvalidInput)
	}
	return nil
}

// MaxAdPhotos keeps ads quick to load.
const MaxAdPhotos = 3

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
	if len(req.Photos) > MaxAdPhotos {
		return Ad{}, fmt.Errorf("%w: at most %d photos per ad", ErrInvalidInput, MaxAdPhotos)
	}
	if err := s.checkSize(ctx, req.SizeDescription); err != nil {
		return Ad{}, err
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
	// The phone's GPS (when sent) beats the nursery's coordinates; the
	// address text still comes from the nursery.
	if req.PickupAddress == nil || req.PickupLatitude == nil {
		addr, lat, lon, err := s.repo.NurseryPickup(ctx, nurseryID)
		if err != nil {
			return Ad{}, err
		}
		if req.PickupAddress == nil {
			req.PickupAddress = addr
		}
		if req.PickupLatitude == nil && lat != nil {
			src := "nursery_default"
			req.PickupLatitude, req.PickupLongitude, req.PickupLocationSource = lat, lon, &src
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
	if err := s.nearbyArea(ctx, actor, &q); err != nil {
		return nil, 0, err
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
		fmt.Sprint(q.Page), fmt.Sprint(q.PerPage), areaKey(q))

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
	if ad.AdType == AdTypeWant && (req.RoadAccessible != nil || req.TruckAccessible != nil || req.AccessInstructions != nil) {
		return Ad{}, fmt.Errorf("%w: road and truck access are only for plants you have", ErrInvalidInput)
	}
	if ad.AdType == AdTypeSell {
		if err := validateAccess(
			firstNonNil(req.RoadAccessible, ad.RoadAccessible),
			firstNonNil(req.TruckAccessible, ad.TruckAccessible),
			firstNonNil(req.PickupLandmark, ad.PickupLandmark),
		); err != nil {
			return Ad{}, err
		}
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
	if len(req.Photos) > MaxAdPhotos {
		return Ad{}, fmt.Errorf("%w: at most %d photos per ad", ErrInvalidInput, MaxAdPhotos)
	}
	if err := s.checkSize(ctx, req.SizeDescription); err != nil {
		return Ad{}, err
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
		// Browsing is by distance: an ad with no location could never be found.
		// Like creation, fall back to the nursery's location -- the error tells
		// owners to add it, so a draft made before must pick it up now.
		if ad.PickupLatitude == nil || ad.PickupLongitude == nil {
			if _, lat, lon, err := s.repo.NurseryPickup(ctx, ad.NurseryID); err == nil && lat != nil && lon != nil {
				if err := s.repo.SetAdPickupLocation(ctx, ad.ID, *lat, *lon, "nursery_default"); err != nil {
					return Ad{}, err
				}
				ad.PickupLatitude, ad.PickupLongitude = lat, lon
			}
		}
		if ad.PickupLatitude == nil || ad.PickupLongitude == nil {
			return Ad{}, fmt.Errorf("%w: this ad has no location; turn on location while posting, or add your nursery's location, so nearby nurseries can find it", ErrInvalidInput)
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
