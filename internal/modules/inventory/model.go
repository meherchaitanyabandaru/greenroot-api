package inventory

import (
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
)

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
	actionDelete = "DELETE"
)

type InventoryItem struct {
	ID                int64                        `json:"id"`
	InventoryCode     string                       `json:"inventory_code"`
	NurseryID         int64                        `json:"nursery_id"`
	NurseryName       string                       `json:"nursery_name"`
	PlantID           int64                        `json:"plant_id"`
	ScientificName    string                       `json:"scientific_name"`
	CommonName        *string                      `json:"common_name,omitempty"`
	SizeID            int16                        `json:"size_id"`
	SizeCode          string                       `json:"size_code"`
	SizeName          string                       `json:"size_name"`
	AvailableQuantity int                          `json:"available_quantity"`
	Status            string                       `json:"inventory_status"`
	LastUpdatedBy     *int64                       `json:"last_updated_by,omitempty"`
	LastUpdatedAt     time.Time                    `json:"last_updated_at"`
	CreatedAt         time.Time                    `json:"created_at"`
	Lifecycle         *lifecycle.InventoryDisplays `json:"lifecycle,omitempty"`
	Capabilities      *InventoryCapabilities       `json:"capabilities,omitempty"`
	Summary           *InventorySummary            `json:"summary,omitempty"`
}

type InventoryCapabilities struct {
	CanEdit        bool `json:"can_edit"`
	CanDelete      bool `json:"can_delete"`
	CanRestock     bool `json:"can_restock"`
	CanReserve     bool `json:"can_reserve"`
	CanDiscontinue bool `json:"can_discontinue"`
}

type InventorySummary struct {
	IsAvailable    bool `json:"is_available"`
	IsLowStock     bool `json:"is_low_stock"`
	IsOutOfStock   bool `json:"is_out_of_stock"`
	IsReserved     bool `json:"is_reserved"`
	IsDiscontinued bool `json:"is_discontinued"`
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}
