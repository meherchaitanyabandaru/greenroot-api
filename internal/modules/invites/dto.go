package invites

type CreateInviteRequest struct {
	InviteType   string  `json:"invite_type"`
	NurseryID    *int64  `json:"nursery_id"`
	Role         *string `json:"role"`
	TargetMobile *string `json:"target_mobile"`
	TargetEmail  *string `json:"target_email"`
	TargetName   *string `json:"target_name"`
}

type InviteResponse struct {
	Invite Invite `json:"invite"`
}

type InvitesResponse struct {
	Invites []Invite `json:"invites"`
}

type MessageResponse struct {
	Message string `json:"message"`
}
