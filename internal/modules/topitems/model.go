// Package topitems is a nursery's showcase: up to MaxPerNursery plants (with
// size, an optional indicative price, quality notes and photos) it is known
// for. It is not stock — orders never reduce it. The sourcing network shows
// them to nearby nurseries.
package topitems

import (
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
)

type ActorContext = authctx.ActorContext

// MaxPerNursery keeps the list a real "top" list and the admin screen small.
const MaxPerNursery = 20

// MaxPhotos and MaxNotesLength bound one item.
const (
	MaxPhotos      = 5
	MaxNotesLength = 500
)

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
	actionDelete = "DELETE"
)

type TopItem struct {
	ID              int64     `json:"id"`
	TopItemCode     string    `json:"top_item_code"`
	NurseryID       int64     `json:"nursery_id"`
	NurseryName     string    `json:"nursery_name"`
	PlantID         int64     `json:"plant_id"`
	ScientificName  string    `json:"scientific_name"`
	CommonName      *string   `json:"common_name,omitempty"`
	SizeID          int16     `json:"size_id"`
	SizeCode        string    `json:"size_code"`
	SizeName        string    `json:"size_name"`
	UnitPrice       *float64  `json:"unit_price,omitempty"`
	DisplayOrder    int16     `json:"display_order"`
	QualityNotes    *string   `json:"quality_notes,omitempty"`
	Photos          []string  `json:"photos"`
	UpdatedByUserID *int64    `json:"updated_by_user_id,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
	CreatedAt       time.Time `json:"created_at"`
	// Version changes on every update; send it back as If-Match to avoid
	// overwriting someone else's change.
	Version      int64         `json:"version"`
	Capabilities *Capabilities `json:"capabilities,omitempty"`
}

type Capabilities struct {
	CanEdit   bool `json:"can_edit"`
	CanDelete bool `json:"can_delete"`
}
