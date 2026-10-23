package invites

import (
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"time"
)

type ActorContext = authctx.ActorContext

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
)

type Invite struct {
	ID              int64   `json:"id"`
	InviteUUID      string  `json:"invite_uuid"`
	InviteType      string  `json:"invite_type"`
	InvitedByUserID int64   `json:"invited_by_user_id"`
	NurseryID       *int64  `json:"nursery_id,omitempty"`
	NurseryName     *string `json:"nursery_name,omitempty"`
	// Who and where, so an invitee can check it is really their nursery
	// before joining (two nurseries can share a name).
	InviterName      *string    `json:"inviter_name,omitempty"`
	OwnerName        *string    `json:"owner_name,omitempty"`
	OwnerMobileHint  *string    `json:"owner_mobile_hint,omitempty"` // last 4 digits only
	NurseryPlace     *string    `json:"nursery_place,omitempty"`     // "Hosur, Tamil Nadu"
	Role             *string    `json:"role,omitempty"`
	TargetMobile     *string    `json:"target_mobile,omitempty"`
	TargetEmail      *string    `json:"target_email,omitempty"`
	TargetName       *string    `json:"target_name,omitempty"`
	Status           string     `json:"status"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	AcceptedByUserID *int64     `json:"accepted_by_user_id,omitempty"`
	AcceptedAt       *time.Time `json:"accepted_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}
