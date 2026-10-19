package plants

import (
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"time"
)

type ActorContext = authctx.ActorContext

type Plant struct {
	ID                 int64      `json:"id"`
	PlantCode          string     `json:"plant_code"`
	ScientificName     string     `json:"scientific_name"`
	CommonName         *string    `json:"common_name,omitempty"`
	EnglishDescription *string    `json:"english_description,omitempty"`
	PlantType          *string    `json:"plant_type,omitempty"`
	LightRequirement   *string    `json:"light_requirement,omitempty"`
	WaterRequirement   *string    `json:"water_requirement,omitempty"`
	WfoID              *string    `json:"wfo_id,omitempty"`
	IsActive           bool       `json:"is_active"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	Categories         []Category `json:"categories,omitempty"`
	Images             []Image    `json:"images,omitempty"`
}

type PlantSize struct {
	ID           int    `json:"id"`
	SizeCode     string `json:"size_code"`
	DisplayName  string `json:"display_name"`
	DisplayOrder int    `json:"display_order"`
	IsActive     bool   `json:"is_active"`
}

type Category struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Image struct {
	ID           int64     `json:"id"`
	PlantID      int64     `json:"plant_id"`
	ImageURL     string    `json:"image_url"`
	AltText      *string   `json:"alt_text,omitempty"`
	DisplayOrder int       `json:"display_order"`
	IsPrimary    bool      `json:"is_primary"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CareGuide struct {
	ID          int64      `json:"id"`
	PlantID     int64      `json:"plant_id"`
	Sunlight    *string    `json:"sunlight,omitempty"`
	Watering    *string    `json:"watering,omitempty"`
	Soil        *string    `json:"soil,omitempty"`
	Temperature *string    `json:"temperature,omitempty"`
	Fertilizer  *string    `json:"fertilizer,omitempty"`
	Pruning     *string    `json:"pruning,omitempty"`
	Notes       *string    `json:"notes,omitempty"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

// BotanicalName is one World Flora Online name (public.botanical_names) that is
// not in the catalog yet. POST /plants/resolve turns it into a catalog plant.
type BotanicalName struct {
	WfoID          string  `json:"wfo_id"`
	ScientificName string  `json:"scientific_name"`
	Authorship     *string `json:"authorship,omitempty"`
	Family         *string `json:"family,omitempty"`
	TaxonRank      string  `json:"taxon_rank"`
	Status         string  `json:"status"`
	// Set for a SYNONYM: resolving it yields the accepted species.
	AcceptedWfoID *string `json:"accepted_wfo_id,omitempty"`
	AcceptedName  *string `json:"accepted_name,omitempty"`
}

// LookupResult is one plant-picker row: exactly one of Plant or Name is set.
type LookupResult struct {
	Plant *Plant         `json:"plant,omitempty"`
	Name  *BotanicalName `json:"name,omitempty"`
}
