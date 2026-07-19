package authctx

import "testing"

func TestHasRole(t *testing.T) {
	a := ActorContext{Roles: []string{"NURSERY_OWNER", "MANAGER"}}
	if !a.HasRole("NURSERY_OWNER") {
		t.Error("expected HasRole(NURSERY_OWNER) = true")
	}
	if !a.HasRole("MANAGER") {
		t.Error("expected HasRole(MANAGER) = true")
	}
	if a.HasRole("DRIVER") {
		t.Error("expected HasRole(DRIVER) = false")
	}
	if a.HasRole("") {
		t.Error("expected HasRole('') = false")
	}
}

func TestHasRole_Empty(t *testing.T) {
	a := ActorContext{}
	if a.HasRole("ADMIN") {
		t.Error("expected HasRole on empty actor = false")
	}
}

func TestAsActorContext(t *testing.T) {
	actor := Actor{
		UserID:        42,
		Mobile:        "9000000000",
		Roles:         []string{"ADMIN", "SUPER_ADMIN"},
		IPAddress:     "1.2.3.4",
		UserAgent:     "test-agent",
		TokenJTI:      "jti-abc",
		TokenExpEpoch: 9999,
	}
	ctx := actor.AsActorContext()
	if ctx.UserID != 42 {
		t.Errorf("UserID: got %d, want 42", ctx.UserID)
	}
	if ctx.Mobile != "9000000000" {
		t.Errorf("Mobile: got %s, want 9000000000", ctx.Mobile)
	}
	if ctx.TokenJTI != "jti-abc" {
		t.Errorf("TokenJTI: got %s, want jti-abc", ctx.TokenJTI)
	}
	if ctx.TokenExpEpoch != 9999 {
		t.Errorf("TokenExpEpoch: got %d, want 9999", ctx.TokenExpEpoch)
	}
	if len(ctx.Roles) != 2 || ctx.Roles[0] != "ADMIN" {
		t.Errorf("Roles mismatch: got %v", ctx.Roles)
	}
}
