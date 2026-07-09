package sourcing

import "time"

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
	actionDelete = "DELETE"
)

// Member represents a nursery's membership in the Plant Sourcing Network.
type Member struct {
	ID              int64     `json:"id"`
	NurseryID       int64     `json:"nursery_id"`
	NurseryName     string    `json:"nursery_name"`
	IsActive        bool      `json:"is_active"`
	RoadAccessible  bool      `json:"road_accessible"`
	LorryAccessible bool      `json:"lorry_accessible"`
	ContactVisible  bool      `json:"contact_visible"`
	ServiceRadiusKM int       `json:"service_radius_km"`
	JoinedAt        time.Time `json:"joined_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// NearbyNursery is a discovery-safe summary of a network member.
// Sensitive business data (customers, orders, financials) is never included.
type NearbyNursery struct {
	NurseryID       int64           `json:"nursery_id"`
	NurseryName     string          `json:"nursery_name"`
	Village         *string         `json:"village,omitempty"`
	DistanceKM      *float64        `json:"distance_km,omitempty"`
	RoadAccessible  bool            `json:"road_accessible"`
	LorryAccessible bool            `json:"lorry_accessible"`
	ContactNumber   *string         `json:"contact_number,omitempty"`
	FeaturedPlants  []FeaturedPlant `json:"featured_plants,omitempty"`
}

// FeaturedPlant is one of the nursery's top-20 "we usually have this" plants.
// NOT inventory — approximate only.
type FeaturedPlant struct {
	ID                  int64     `json:"id"`
	NurseryID           int64     `json:"nursery_id"`
	PlantID             int64     `json:"plant_id"`
	PlantName           string    `json:"plant_name"`
	DisplayOrder        int16     `json:"display_order"`
	ApproximateQuantity *int      `json:"approximate_quantity,omitempty"`
	ApproximateSize     *string   `json:"approximate_size,omitempty"`
	QualityNotes        *string   `json:"quality_notes,omitempty"`
	Photos              []string  `json:"photos"`
	IsActive            bool      `json:"is_active"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// SourcingPost is a NEED or AVAILABLE announcement broadcast to nearby members.
type SourcingPost struct {
	ID             int64       `json:"id"`
	PostCode       string      `json:"post_code"`
	NurseryID      int64       `json:"nursery_id"`
	NurseryName    string      `json:"nursery_name"`
	PostedByUserID int64       `json:"posted_by_user_id"`
	PostedByName   string      `json:"posted_by_name"`
	PostType       string      `json:"post_type"` // NEED | AVAILABLE
	PlantID        *int64      `json:"plant_id,omitempty"`
	PlantName      string      `json:"plant_name"`
	SizeDesc       *string     `json:"size_description,omitempty"`
	Quantity       *int        `json:"quantity,omitempty"`
	Urgency        string      `json:"urgency"` // TODAY | URGENT | FLEXIBLE
	NeededByDate   *time.Time  `json:"needed_by_date,omitempty"`
	Notes          *string     `json:"notes,omitempty"`
	RadiusKM       int         `json:"radius_km"`
	ResponseCount  int         `json:"response_count"`
	Status         string      `json:"status"` // OPEN | CLOSED | EXPIRED
	ExpiresAt      *time.Time  `json:"expires_at,omitempty"`
	ClosedAt       *time.Time  `json:"closed_at,omitempty"`
	Photos         []PostPhoto `json:"photos,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

// PostPhoto is a photo attached to a SourcingPost.
type PostPhoto struct {
	ID           int64     `json:"id"`
	PostID       int64     `json:"post_id"`
	PhotoURL     string    `json:"photo_url"`
	DisplayOrder int16     `json:"display_order"`
	CreatedAt    time.Time `json:"created_at"`
}

// PostResponse is a reply from another network member to an open SourcingPost.
type PostResponse struct {
	ID                 int64     `json:"id"`
	PostID             int64     `json:"post_id"`
	ResponderNurseryID int64     `json:"responder_nursery_id"`
	ResponderNursery   string    `json:"responder_nursery"`
	RespondedByUserID  int64     `json:"responded_by_user_id"`
	RespondedByName    string    `json:"responded_by_name"`
	AvailableQuantity  *int      `json:"available_quantity,omitempty"`
	Notes              *string   `json:"notes,omitempty"`
	ContactInfo        *string   `json:"contact_info,omitempty"`
	Status             string    `json:"status"` // PENDING | ACCEPTED | DECLINED
	RespondedAt        time.Time `json:"responded_at"`
	CreatedAt          time.Time `json:"created_at"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}

// ---- Request DTOs ----

type JoinNetworkRequest struct {
	RoadAccessible  bool `json:"road_accessible"`
	LorryAccessible bool `json:"lorry_accessible"`
	ContactVisible  bool `json:"contact_visible"`
	ServiceRadiusKM int  `json:"service_radius_km"`
}

type NearbyQuery struct {
	Latitude  *float64 `json:"-"`
	Longitude *float64 `json:"-"`
	RadiusKM  int      `json:"-"`
	PlantName string   `json:"-"`
	Page      int      `json:"-"`
	PerPage   int      `json:"-"`
}

type CreateFeaturedPlantRequest struct {
	PlantID             int64    `json:"plant_id"`
	DisplayOrder        int16    `json:"display_order"`
	ApproximateQuantity *int     `json:"approximate_quantity,omitempty"`
	ApproximateSize     *string  `json:"approximate_size,omitempty"`
	QualityNotes        *string  `json:"quality_notes,omitempty"`
	Photos              []string `json:"photos"`
}

type UpdateFeaturedPlantRequest struct {
	DisplayOrder        int16    `json:"display_order"`
	ApproximateQuantity *int     `json:"approximate_quantity,omitempty"`
	ApproximateSize     *string  `json:"approximate_size,omitempty"`
	QualityNotes        *string  `json:"quality_notes,omitempty"`
	Photos              []string `json:"photos"`
	IsActive            bool     `json:"is_active"`
}

type CreatePostRequest struct {
	NurseryID    int64   `json:"nursery_id"`
	PostType     string  `json:"post_type"`
	PlantID      *int64  `json:"plant_id,omitempty"`
	PlantName    string  `json:"plant_name"`
	SizeDesc     *string `json:"size_description,omitempty"`
	Quantity     *int    `json:"quantity,omitempty"`
	Urgency      string  `json:"urgency"`
	NeededByDate *string `json:"needed_by_date,omitempty"`
	Notes        *string `json:"notes,omitempty"`
	RadiusKM     int     `json:"radius_km"`
	ExpiresAt    *string `json:"expires_at,omitempty"`
}

type UpdatePostRequest struct {
	PlantName string  `json:"plant_name"`
	SizeDesc  *string `json:"size_description,omitempty"`
	Quantity  *int    `json:"quantity,omitempty"`
	Urgency   string  `json:"urgency"`
	Notes     *string `json:"notes,omitempty"`
	Status    string  `json:"status"`
}

type ListPostsQuery struct {
	NurseryID int64  `json:"-"`
	PostType  string `json:"-"`
	Status    string `json:"-"`
	PlantName string `json:"-"`
	Page      int    `json:"-"`
	PerPage   int    `json:"-"`
}

type CreateResponseRequest struct {
	ResponderNurseryID int64   `json:"responder_nursery_id"`
	AvailableQuantity  *int    `json:"available_quantity,omitempty"`
	Notes              *string `json:"notes,omitempty"`
	ContactInfo        *string `json:"contact_info,omitempty"`
}

type UpdateResponseRequest struct {
	Status string `json:"status"` // ACCEPTED | DECLINED
}

// ---- Response wrappers ----

type MemberResponse struct {
	Member Member `json:"member"`
}

type NearbyNurseriesResponse struct {
	Nurseries  []NearbyNursery `json:"nurseries"`
	Pagination Pagination      `json:"pagination"`
}

type FeaturedPlantResponse struct {
	FeaturedPlant FeaturedPlant `json:"featured_plant"`
}

type FeaturedPlantsResponse struct {
	FeaturedPlants []FeaturedPlant `json:"featured_plants"`
}

type PostWrapResponse struct {
	Post SourcingPost `json:"post"`
}

type PostsResponse struct {
	Posts      []SourcingPost `json:"posts"`
	Pagination Pagination     `json:"pagination"`
}

type ResponseWrap struct {
	Response PostResponse `json:"response"`
}

type ResponsesWrap struct {
	Responses []PostResponse `json:"responses"`
}

