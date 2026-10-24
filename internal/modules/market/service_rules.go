// Market ad validation and presentation helpers.
package market

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
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
		return validateAccess(req.RoadAccessible, req.TruckAccessible, req.PickupLandmark)
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
		if req.RoadAccessible != nil || req.TruckAccessible != nil || req.AccessInstructions != nil {
			return fmt.Errorf("%w: road and truck access are only for plants you have", ErrInvalidInput)
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

// validateAccess checks a for-sale ad's pickup answers. Road and truck are
// separate (a road may be too narrow for a truck), but a truck needs a road,
// and with no road the seller must name a pickup point buyers can reach.
func validateAccess(road, truck *bool, pickupPoint *string) error {
	if truck != nil && *truck && road != nil && !*road {
		return fmt.Errorf("%w: a truck cannot reach a place with no road", ErrInvalidInput)
	}
	if road != nil && !*road && (pickupPoint == nil || strings.TrimSpace(*pickupPoint) == "") {
		return fmt.Errorf("%w: no road to the plants: give a pickup point buyers can reach", ErrInvalidInput)
	}
	return nil
}

// areaKey keeps cached search pages per area (about 100 m and 1 km steps).
func areaKey(q AdsQuery) string {
	if q.NearLat == nil || q.NearLon == nil || q.RadiusKM == nil {
		return "all"
	}
	return fmt.Sprintf("%.3f,%.3f,%.0f", *q.NearLat, *q.NearLon, *q.RadiusKM)
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

func canAccessMarket(actor ActorContext) bool {
	return actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER") ||
		actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN")
}

// firstNonNil is the value an update leaves: the request's, else the stored one.
func firstNonNil[T any](a, b *T) *T {
	if a != nil {
		return a
	}
	return b
}

func canDeleteAd(ad Ad) bool {
	return ad.Status == StatusDraft ||
		((ad.Status == StatusPublished || ad.Status == StatusPaused) && ad.EnquiryCount == 0)
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

func daysRemaining(expiresAt *time.Time) *int {
	if expiresAt == nil {
		return nil
	}
	days := int(time.Until(*expiresAt).Hours() / 24)
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
