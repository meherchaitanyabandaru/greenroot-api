package plantrequests

import (
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
)

type ActorContext = authctx.ActorContext

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
	actionDelete = "DELETE"
)

type PlantRequest struct {
	ID                  int64                           `json:"id"`
	RequestCode         string                          `json:"request_code"`
	RequestingNurseryID int64                           `json:"requesting_nursery_id"`
	RequestingNursery   string                          `json:"requesting_nursery"`
	RequestedByUserID   int64                           `json:"requested_by_user_id"`
	RequestedByName     string                          `json:"requested_by_name"`
	PlantID             int64                           `json:"plant_id"`
	ScientificName      string                          `json:"scientific_name"`
	CommonName          *string                         `json:"common_name,omitempty"`
	SizeID              *int16                          `json:"size_id,omitempty"`
	SizeCode            *string                         `json:"size_code,omitempty"`
	SizeName            *string                         `json:"size_name,omitempty"`
	QuantityRequired    int                             `json:"quantity_required"`
	RadiusKM            int                             `json:"radius_km"`
	RequiredByDate      *time.Time                      `json:"required_by_date,omitempty"`
	Notes               *string                         `json:"notes,omitempty"`
	Status              string                          `json:"status"`
	ExpiresAt           *time.Time                      `json:"expires_at,omitempty"`
	FulfilledAt         *time.Time                      `json:"fulfilled_at,omitempty"`
	CreatedAt           time.Time                       `json:"created_at"`
	UpdatedAt           time.Time                       `json:"updated_at"`
	Responses           []Response                      `json:"responses,omitempty"`
	Lifecycle           *lifecycle.PlantRequestDisplays `json:"lifecycle,omitempty"`
	Capabilities        *PlantRequestCapabilities       `json:"capabilities,omitempty"`
	Summary             *PlantRequestSummary            `json:"summary,omitempty"`
}

type PlantRequestCapabilities struct {
	CanEdit           bool `json:"can_edit"`
	CanDelete         bool `json:"can_delete"`
	CanPublish        bool `json:"can_publish"`
	CanClose          bool `json:"can_close"`
	CanReject         bool `json:"can_reject"`
	CanRespond        bool `json:"can_respond"`
	CanAcceptResponse bool `json:"can_accept_response"`
	CanRejectResponse bool `json:"can_reject_response"`
}

type PlantRequestSummary struct {
	ResponseCount         int `json:"response_count"`
	AcceptedResponseCount int `json:"accepted_response_count"`
	AvailableQuantity     int `json:"available_quantity"`
	AcceptedQuantity      int `json:"accepted_quantity"`
	RemainingQuantity     int `json:"remaining_quantity"`
}

type Response struct {
	ID                int64     `json:"id"`
	RequestID         int64     `json:"request_id"`
	SupplierNurseryID int64     `json:"supplier_nursery_id"`
	SupplierNursery   string    `json:"supplier_nursery"`
	RespondedByUserID int64     `json:"responded_by_user_id"`
	RespondedByName   string    `json:"responded_by_name"`
	AvailableQuantity int       `json:"available_quantity"`
	Remarks           *string   `json:"remarks,omitempty"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
}
