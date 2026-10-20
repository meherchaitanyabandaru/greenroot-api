package market

import (
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
)

type ActorContext = authctx.ActorContext

// ── Actor ────────────────────────────────────────────────────

// ── Ad ───────────────────────────────────────────────────────

// Ad statuses.
const (
	StatusDraft     = "DRAFT"
	StatusPublished = "PUBLISHED"
	StatusPaused    = "PAUSED"
	StatusExpired   = "EXPIRED"
	StatusArchived  = "ARCHIVED"
	// Moderation: SUSPENDED hides an ad while GreenRoot reviews a serious
	// report; REMOVED is final (the report was upheld).
	StatusSuspended = "SUSPENDED"
	StatusRemoved   = "REMOVED"
)

// AdExpireDays is how long a published ad stays live before auto-expiring.
const AdExpireDays = 30

type Ad struct {
	ID                   int64                       `json:"id"`
	Code                 string                      `json:"code"`
	NurseryID            int64                       `json:"nursery_id"`
	NurseryName          string                      `json:"nursery_name"`
	NurseryVerified      bool                        `json:"nursery_verified"`
	NurseryMobile        *string                     `json:"nursery_mobile,omitempty"`
	CreatedByUserID      int64                       `json:"created_by_user_id"`
	PlantID              *int64                      `json:"plant_id,omitempty"`
	PlantName            string                      `json:"plant_name"`
	CategoryName         *string                     `json:"category_name,omitempty"`
	Title                string                      `json:"title"`
	Description          *string                     `json:"description,omitempty"`
	Quantity             *int                        `json:"quantity,omitempty"`
	SizeDescription      *string                     `json:"size_description,omitempty"`
	PricePerUnit         *float64                    `json:"price_per_unit,omitempty"`
	PriceUnit            *string                     `json:"price_unit,omitempty"`
	Photos               []string                    `json:"photos"`
	PickupAddress        *string                     `json:"pickup_address,omitempty"`
	PickupLandmark       *string                     `json:"pickup_landmark,omitempty"`
	PickupLatitude       *float64                    `json:"pickup_latitude,omitempty"`
	PickupLongitude      *float64                    `json:"pickup_longitude,omitempty"`
	PickupGPSAccuracyM   *float64                    `json:"pickup_gps_accuracy_meters,omitempty"`
	PickupLocationSource *string                     `json:"pickup_location_source,omitempty"`
	PickupConfirmedBy    *int64                      `json:"pickup_confirmed_by,omitempty"`
	PickupConfirmedAt    *time.Time                  `json:"pickup_confirmed_at,omitempty"`
	Status               string                      `json:"status"`
	ViewCount            int                         `json:"view_count"`
	SaveCount            int                         `json:"save_count"`
	EnquiryCount         int                         `json:"enquiry_count"`
	IsSavedByMe          bool                        `json:"is_saved_by_me"`
	MyEnquiryID          *int64                      `json:"my_enquiry_id,omitempty"` // the viewer's chat about this ad, if any
	ExpiresAt            *time.Time                  `json:"expires_at,omitempty"`
	PublishedAt          *time.Time                  `json:"published_at,omitempty"`
	PausedAt             *time.Time                  `json:"paused_at,omitempty"`
	ExpiredAt            *time.Time                  `json:"expired_at,omitempty"`
	ArchivedAt           *time.Time                  `json:"archived_at,omitempty"`
	ModerationNote       *string                     `json:"moderation_note,omitempty"` // why GreenRoot suspended or removed it
	PostedByName         *string                     `json:"posted_by_name,omitempty"`
	PostedByPhoto        *string                     `json:"posted_by_photo,omitempty"` // the poster's camera selfie
	CreatedAt            time.Time                   `json:"created_at"`
	UpdatedAt            time.Time                   `json:"updated_at"`
	Lifecycle            *lifecycle.MarketAdDisplays `json:"lifecycle,omitempty"`
	Capabilities         *AdCapabilities             `json:"capabilities,omitempty"`
	Summary              *AdSummary                  `json:"summary,omitempty"`
}

type AdCapabilities struct {
	CanEdit    bool `json:"can_edit"`
	CanPublish bool `json:"can_publish"`
	CanPause   bool `json:"can_pause"`
	CanResume  bool `json:"can_resume"`
	CanRenew   bool `json:"can_renew"`
	CanArchive bool `json:"can_archive"`
	CanSave    bool `json:"can_save"`
	CanEnquire bool `json:"can_enquire"` // false when the viewer already has a chat: open MyEnquiryID instead
	CanReport  bool `json:"can_report"`
}

type AdSummary struct {
	IsOwner       bool `json:"is_owner"`
	IsLive        bool `json:"is_live"`
	IsExpired     bool `json:"is_expired"`
	DaysRemaining *int `json:"days_remaining,omitempty"`
}

// SellerProfile is what a nursery shows other nurseries on the market.
type SellerProfile struct {
	NurseryID   int64     `json:"nursery_id"`
	NurseryName string    `json:"nursery_name"`
	Verified    bool      `json:"verified"`
	Description *string   `json:"description,omitempty"`
	LogoURL     *string   `json:"logo_url,omitempty"`
	City        *string   `json:"city,omitempty"`
	OwnerName   *string   `json:"owner_name,omitempty"`
	OwnerPhoto  *string   `json:"owner_photo,omitempty"`
	MemberSince time.Time `json:"member_since"`
	AdsPosted   int       `json:"ads_posted"`
	LiveAds     int       `json:"live_ads"`
	DealsAgreed int       `json:"deals_agreed"`
	Ads         []Ad      `json:"ads"` // live ads, newest first
	// TopItems is the nursery's showcase (up to 20 plants it is known for).
	TopItems []SellerTopItem `json:"top_items"`
}

type SellerTopItem struct {
	PlantID      int64    `json:"plant_id"`
	PlantName    string   `json:"plant_name"`
	CommonName   *string  `json:"common_name,omitempty"`
	SizeName     string   `json:"size_name"`
	UnitPrice    *float64 `json:"unit_price,omitempty"`
	QualityNotes *string  `json:"quality_notes,omitempty"`
	Photo        *string  `json:"photo,omitempty"`
}

// ── Enquiry ──────────────────────────────────────────────────

// Enquiry statuses.
const (
	EnquiryNew        = "NEW"
	EnquiryInProgress = "IN_PROGRESS"
	EnquiryDealAgreed = "DEAL_AGREED" // an offer was accepted; the chat stays open to arrange pickup
	EnquiryClosed     = "CLOSED"
	EnquiryCancelled  = "CANCELLED"
)

// Chat message types and offer statuses.
const (
	MessageText   = "TEXT"
	MessageOffer  = "OFFER"
	MessageSystem = "SYSTEM"

	OfferPending   = "PENDING"
	OfferAccepted  = "ACCEPTED"
	OfferDeclined  = "DECLINED"
	OfferWithdrawn = "WITHDRAWN" // replaced by a newer offer in the same chat
)

type Enquiry struct {
	ID                 int64                            `json:"id"`
	Code               string                           `json:"code"`
	AdID               int64                            `json:"ad_id"`
	AdTitle            string                           `json:"ad_title"`
	AdStatus           string                           `json:"ad_status"`
	AdPricePerUnit     *float64                         `json:"ad_price_per_unit,omitempty"`
	AdPriceUnit        *string                          `json:"ad_price_unit,omitempty"`
	AdQuantity         *int                             `json:"ad_quantity,omitempty"`
	AdPhoto            *string                          `json:"ad_photo,omitempty"`
	AdNurseryID        int64                            `json:"ad_nursery_id"`
	AdNurseryName      string                           `json:"ad_nursery_name"`
	EnquiringNurseryID int64                            `json:"enquiring_nursery_id"`
	EnquiryNurseryName string                           `json:"enquiring_nursery_name"`
	CreatedByUserID    int64                            `json:"created_by_user_id"`
	Message            string                           `json:"message"`
	QuantityNeeded     *int                             `json:"quantity_needed,omitempty"`
	Status             string                           `json:"status"`
	AgreedPricePerUnit *float64                         `json:"agreed_price_per_unit,omitempty"`
	AgreedQuantity     *int                             `json:"agreed_quantity,omitempty"`
	AgreedAt           *time.Time                       `json:"agreed_at,omitempty"`
	ViewedAt           *time.Time                       `json:"viewed_at,omitempty"`
	RepliedAt          *time.Time                       `json:"replied_at,omitempty"`
	Messages           []Message                        `json:"messages,omitempty"`
	CreatedAt          time.Time                        `json:"created_at"`
	UpdatedAt          time.Time                        `json:"updated_at"`
	Lifecycle          *lifecycle.MarketEnquiryDisplays `json:"lifecycle,omitempty"`
	Capabilities       *EnquiryCapabilities             `json:"capabilities,omitempty"`
	Summary            *EnquirySummary                  `json:"summary,omitempty"`
}

type EnquiryCapabilities struct {
	CanReply  bool `json:"can_reply"`
	CanOffer  bool `json:"can_offer"`
	CanClose  bool `json:"can_close"`
	CanCancel bool `json:"can_cancel"`
	// ReadOnlyReason says why nobody can write, e.g. the ad is no longer live.
	ReadOnlyReason string `json:"read_only_reason,omitempty"`
}

type EnquirySummary struct {
	IsBuyer  bool `json:"is_buyer"`
	IsSeller bool `json:"is_seller"`
	IsOpen   bool `json:"is_open"`
}

type Message struct {
	ID              int64     `json:"id"`
	EnquiryID       int64     `json:"enquiry_id"`
	SentByUserID    int64     `json:"sent_by_user_id"`
	SentByNurseryID int64     `json:"sent_by_nursery_id"`
	NurseryName     string    `json:"nursery_name"`
	SenderName      string    `json:"sender_name"`
	Type            string    `json:"type"`
	Body            string    `json:"body"`
	OfferPrice      *float64  `json:"offer_price_per_unit,omitempty"`
	OfferQuantity   *int      `json:"offer_quantity,omitempty"`
	OfferStatus     *string   `json:"offer_status,omitempty"`
	IsMine          bool      `json:"is_mine"`
	CanRespond      bool      `json:"can_respond"` // the viewer can accept or decline this offer
	CreatedAt       time.Time `json:"created_at"`
}

// ── Request DTOs ─────────────────────────────────────────────

type CreateAdRequest struct {
	PlantID              *int64     `json:"plant_id"`
	PlantName            string     `json:"plant_name"`
	CategoryName         *string    `json:"category_name"`
	Title                string     `json:"title"`
	Description          *string    `json:"description"`
	Quantity             *int       `json:"quantity"`
	SizeDescription      *string    `json:"size_description"`
	PricePerUnit         *float64   `json:"price_per_unit"`
	PriceUnit            *string    `json:"price_unit"`
	Photos               []string   `json:"photos"`
	PickupAddress        *string    `json:"pickup_address"`
	PickupLandmark       *string    `json:"pickup_landmark"`
	PickupLatitude       *float64   `json:"pickup_latitude"`
	PickupLongitude      *float64   `json:"pickup_longitude"`
	PickupGPSAccuracyM   *float64   `json:"pickup_gps_accuracy_meters"`
	PickupLocationSource *string    `json:"pickup_location_source"`
	PickupConfirmedBy    *int64     `json:"pickup_confirmed_by,omitempty"`
	PickupConfirmedAt    *time.Time `json:"pickup_confirmed_at,omitempty"`
}

type UpdateAdRequest struct {
	PlantID              *int64     `json:"plant_id"`
	PlantName            *string    `json:"plant_name"`
	CategoryName         *string    `json:"category_name"`
	Title                *string    `json:"title"`
	Description          *string    `json:"description"`
	Quantity             *int       `json:"quantity"`
	SizeDescription      *string    `json:"size_description"`
	PricePerUnit         *float64   `json:"price_per_unit"`
	PriceUnit            *string    `json:"price_unit"`
	Photos               []string   `json:"photos"`
	PickupAddress        *string    `json:"pickup_address"`
	PickupLandmark       *string    `json:"pickup_landmark"`
	PickupLatitude       *float64   `json:"pickup_latitude"`
	PickupLongitude      *float64   `json:"pickup_longitude"`
	PickupGPSAccuracyM   *float64   `json:"pickup_gps_accuracy_meters"`
	PickupLocationSource *string    `json:"pickup_location_source"`
	PickupConfirmedBy    *int64     `json:"pickup_confirmed_by,omitempty"`
	PickupConfirmedAt    *time.Time `json:"pickup_confirmed_at,omitempty"`
}

type CreateEnquiryRequest struct {
	Message        string `json:"message"`
	QuantityNeeded *int   `json:"quantity_needed"`
}

type ReplyEnquiryRequest struct {
	Body string `json:"body"`
}

type ReportAdRequest struct {
	Reason string  `json:"reason"`
	Notes  *string `json:"notes"`
}

type MakeOfferRequest struct {
	PricePerUnit float64 `json:"price_per_unit"`
	Quantity     *int    `json:"quantity"`
}

// ── List queries ─────────────────────────────────────────────

type AdsQuery struct {
	Search   string  // full-text across title, plant_name, nursery_name, description
	Sort     string  // newest|oldest|price_asc|price_desc|popular|nearest
	Category string  // exact match on category_name (case-insensitive)
	MinPrice float64 // 0 = no lower bound
	MaxPrice float64 // 0 = no upper bound
	// Nearby search: all three must be set together to activate.
	NearLat  *float64 // buyer's latitude
	NearLon  *float64 // buyer's longitude
	RadiusKM *float64 // search radius in km; defaults to 50 when NearLat/NearLon are set
	Page     int
	PerPage  int
}

type EnquiriesQuery struct {
	Direction string // "received" | "sent" | "" (both)
	Status    string
	AdID      int64 // 0 = all ads; the seller's chats about one ad
	Page      int
	PerPage   int
}

// ── Response wrappers ────────────────────────────────────────

type AdResponse struct {
	Ad Ad `json:"ad"`
}

type AdsResponse struct {
	Ads     []Ad `json:"ads"`
	Total   int  `json:"total"`
	Page    int  `json:"page"`
	PerPage int  `json:"per_page"`
}

type EnquiryResponse struct {
	Enquiry Enquiry `json:"enquiry"`
}

type EnquiriesResponse struct {
	Enquiries []Enquiry `json:"enquiries"`
	Total     int       `json:"total"`
	Page      int       `json:"page"`
	PerPage   int       `json:"per_page"`
}

type SaveToggleResponse struct {
	Saved bool `json:"saved"`
}
