package sourcing

import (
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"time"
)

type ActorContext = authctx.ActorContext

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
	NurseryID       int64     `json:"nursery_id"`
	NurseryName     string    `json:"nursery_name"`
	Village         *string   `json:"village,omitempty"`
	DistanceKM      *float64  `json:"distance_km,omitempty"`
	RoadAccessible  bool      `json:"road_accessible"`
	LorryAccessible bool      `json:"lorry_accessible"`
	ContactNumber   *string   `json:"contact_number,omitempty"`
	TopItems        []TopItem `json:"top_items"`
}

// TopItem is the network view of one of the nursery's top items
// (topitems module). Not stock — a showcase.
type TopItem struct {
	PlantID      int64    `json:"plant_id"`
	PlantName    string   `json:"plant_name"`
	SizeName     string   `json:"size_name"`
	UnitPrice    *float64 `json:"unit_price,omitempty"`
	QualityNotes *string  `json:"quality_notes,omitempty"`
	Photos       []string `json:"photos"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
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

// ---- Response wrappers ----

type MemberResponse struct {
	Member Member `json:"member"`
}

type NearbyNurseriesResponse struct {
	Nurseries  []NearbyNursery `json:"nurseries"`
	Pagination Pagination      `json:"pagination"`
}
