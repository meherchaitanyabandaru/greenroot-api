package invites

import (
	"time"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
)

type ActorContext = authctx.ActorContext


const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
)

type Invite struct {
	ID               int64      `json:"id"`
	InviteUUID       string     `json:"invite_uuid"`
	InviteType       string     `json:"invite_type"`
	InvitedByUserID  int64      `json:"invited_by_user_id"`
	NurseryID        *int64     `json:"nursery_id,omitempty"`
	NurseryName      *string    `json:"nursery_name,omitempty"`
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
