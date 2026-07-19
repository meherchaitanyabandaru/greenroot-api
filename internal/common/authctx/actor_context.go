package authctx

// ActorContext is the lean service-layer representation of the caller.
// It carries only what business logic needs — no JWT internals.
// Convert from the full JWT-backed Actor using actor.AsActorContext().
type ActorContext struct {
	UserID    int64
	Mobile    string // needed by invites module
	Roles     []string
	IPAddress string
	UserAgent string
	// TokenJTI and TokenExpEpoch are populated only when the users module
	// needs to revoke a session (account deletion / logout).
	TokenJTI      string
	TokenExpEpoch int64
}

// HasRole reports whether the actor holds the named role.
func (a ActorContext) HasRole(role string) bool {
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// AsActorContext converts the full JWT-backed Actor into the lean service actor.
func (a Actor) AsActorContext() ActorContext {
	return ActorContext{
		UserID:        a.UserID,
		Mobile:        a.Mobile,
		Roles:         a.Roles,
		IPAddress:     a.IPAddress,
		UserAgent:     a.UserAgent,
		TokenJTI:      a.TokenJTI,
		TokenExpEpoch: a.TokenExpEpoch,
	}
}
