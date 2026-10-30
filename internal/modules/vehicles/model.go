package vehicles

import (
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
)

type ActorContext = authctx.ActorContext

type Vehicle struct {
	ID                   int64                      `json:"id"`
	VehicleCode          string                     `json:"vehicle_code"`
	VehicleNumber        string                     `json:"vehicle_number"`
	VehicleType          *string                    `json:"vehicle_type,omitempty"`
	CapacityKG           *float64                   `json:"capacity_kg,omitempty"`
	OwnerID              *int64                     `json:"owner_id,omitempty"`
	OwnerName            *string                    `json:"owner_name,omitempty"`
	Mobile               *string                    `json:"mobile,omitempty"` // owner's mobile
	OwnerHasAccount      bool                       `json:"owner_has_account"`
	ApprovalStatus       string                     `json:"approval_status"`
	ApprovalNote         *string                    `json:"approval_note,omitempty"`
	DriverCount          int                        `json:"driver_count"` // approved driver links
	IsOnActiveTrip       bool                       `json:"is_on_active_trip"`
	DriverID             *int64                     `json:"driver_id,omitempty"`
	DriverName           *string                    `json:"driver_name,omitempty"`
	DriverMobile         *string                    `json:"driver_mobile,omitempty"`
	DriverApprovalStatus *string                    `json:"driver_approval_status,omitempty"`
	Status               string                     `json:"status"`
	CreatedAt            time.Time                  `json:"created_at"`
	UpdatedAt            *time.Time                 `json:"updated_at,omitempty"`
	Lifecycle            *lifecycle.VehicleDisplays `json:"lifecycle,omitempty"`
	Capabilities         *VehicleCapabilities       `json:"capabilities,omitempty"`
	Summary              *VehicleSummary            `json:"summary,omitempty"`
}

type VehicleCapabilities struct {
	CanEdit   bool `json:"can_edit"`
	CanDelete bool `json:"can_delete"`
	CanRetire bool `json:"can_retire"`
	CanTrack  bool `json:"can_track"`
}

type VehicleSummary struct {
	IsActive      bool `json:"is_active"`
	IsInactive    bool `json:"is_inactive"`
	IsMaintenance bool `json:"is_maintenance"`
	IsRetired     bool `json:"is_retired"`
	IsAssigned    bool `json:"is_assigned"`
}
