package apitest

import (
	"fmt"
	"net/http"
	"testing"
)

// ─── Auth guard (lifecycle endpoints) ─────────────────────────────────────────

func TestLeaveNursery_RequiresAuth(t *testing.T) {
	resp := deleteReq(t, "/api/v1/nurseries/me/leave", "")
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestDisconnectDriver_RequiresAuth(t *testing.T) {
	resp := deleteReq(t, "/api/v1/nurseries/1/drivers/1", "")
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestDeleteAccount_RequiresAuth(t *testing.T) {
	resp := deleteReq(t, "/api/v1/users/me", "")
	assertStatus(t, resp, http.StatusUnauthorized)
}

// ─── RBAC guards ──────────────────────────────────────────────────────────────

func TestListDrivers_BuyerForbidden(t *testing.T) {
	token := login(t, buyerPhone)
	resp := get(t, "/api/v1/nurseries/1/drivers", token)
	// Buyer cannot list another nursery's drivers
	assertStatus(t, resp, http.StatusForbidden)
}

func TestListManagers_BuyerForbidden(t *testing.T) {
	token := login(t, buyerPhone)
	resp := get(t, "/api/v1/nurseries/1/managers", token)
	assertStatus(t, resp, http.StatusForbidden)
}

func TestDisconnectDriver_BuyerForbidden(t *testing.T) {
	token := login(t, buyerPhone)
	resp := deleteReq(t, "/api/v1/nurseries/1/drivers/1", token)
	assertStatus(t, resp, http.StatusForbidden)
}

func TestDisconnectDriver_ManagerForbidden(t *testing.T) {
	token := login(t, managerPhone)
	resp := deleteReq(t, "/api/v1/nurseries/1/drivers/1", token)
	// manager cannot disconnect a driver — only owner / driver-self / admin
	assertStatus(t, resp, http.StatusForbidden)
}

// ─── Owner can list their managers and drivers ─────────────────────────────────

func TestListManagers_OwnerSuccess(t *testing.T) {
	token := login(t, ownerPhone)

	nurseryID := ownerNurseryID(t, token)
	resp := get(t, fmt.Sprintf("/api/v1/nurseries/%d/managers", nurseryID), token)
	assertStatus(t, resp, http.StatusOK)
}

func TestListDrivers_OwnerSuccess(t *testing.T) {
	token := login(t, ownerPhone)

	nurseryID := ownerNurseryID(t, token)
	resp := get(t, fmt.Sprintf("/api/v1/nurseries/%d/drivers", nurseryID), token)
	assertStatus(t, resp, http.StatusOK)
}

// ─── LeaveNursery — non-member gets 4xx ───────────────────────────────────────

func TestLeaveNursery_BuyerHasNoNursery(t *testing.T) {
	// A buyer (no nursery membership) gets a 4xx — either 400 or 404.
	token := login(t, buyerPhone)
	resp := deleteReq(t, "/api/v1/nurseries/me/leave", token)
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Errorf("buyer leaving non-existent nursery should be 4xx, got %d", resp.StatusCode)
	}
}

// ─── Account deletion — full round-trip with a fresh phone number ──────────────

// TestDeleteAccount_ThenReregister exercises the complete account lifecycle:
// 1. Register with a fresh phone number (OTP flow)
// 2. Delete the account → 200
// 3. Re-register with the same phone number → 200 with a new user ID
//
// This test is deliberately non-destructive to the seeded accounts and
// uses a unique phone prefix that doesn't appear in the seed file.
func TestDeleteAccount_ThenReregister(t *testing.T) {
	freshPhone := "9501000001"

	// 1. Register: send OTP, verify OTP → get access token
	sendOTP(t, freshPhone)
	body := map[string]string{"mobile": freshPhone, "otp": devOTP}
	verifyResp := post(t, "/api/v1/auth/verify-otp", body, "")
	assertStatus(t, verifyResp, http.StatusOK)

	var firstLogin struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	decode(t, verifyResp, &firstLogin)
	if firstLogin.AccessToken == "" {
		t.Fatal("expected access token after first registration")
	}
	firstUserID := firstLogin.User.ID

	// 2. Delete the account
	delResp := deleteReq(t, "/api/v1/users/me", firstLogin.AccessToken)
	assertStatus(t, delResp, http.StatusOK)

	// 3. Re-register with the same phone number
	sendOTP(t, freshPhone)
	reVerifyResp := post(t, "/api/v1/auth/verify-otp", map[string]string{"mobile": freshPhone, "otp": devOTP}, "")
	assertStatus(t, reVerifyResp, http.StatusOK)

	var secondLogin struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	decode(t, reVerifyResp, &secondLogin)

	if secondLogin.User.ID == firstUserID {
		t.Errorf("re-registered user must have a new ID; got the same ID %d", firstUserID)
	}
	if secondLogin.User.ID == 0 {
		t.Error("re-registered user must have a valid ID")
	}
}

// ─── Deleted account cannot authenticate ──────────────────────────────────────

func TestDeleteAccount_TokenInvalidatedAfterDeletion(t *testing.T) {
	freshPhone := "9501000002"

	// Register
	sendOTP(t, freshPhone)
	verifyResp := post(t, "/api/v1/auth/verify-otp", map[string]string{"mobile": freshPhone, "otp": devOTP}, "")
	assertStatus(t, verifyResp, http.StatusOK)

	var login struct {
		AccessToken string `json:"access_token"`
	}
	decode(t, verifyResp, &login)
	token := login.AccessToken

	// Delete
	delResp := deleteReq(t, "/api/v1/users/me", token)
	assertStatus(t, delResp, http.StatusOK)

	// Try to access /users/me with the old token — should be blocked immediately.
	meResp := get(t, "/api/v1/users/me", token)
	assertStatus(t, meResp, http.StatusForbidden)
}

// ─── helper ───────────────────────────────────────────────────────────────────

// ownerNurseryID fetches the OWNED_NURSERY workspace and returns the nursery ID.
func ownerNurseryID(t *testing.T, token string) int64 {
	t.Helper()
	for _, w := range getWorkspaces(t, token) {
		if w.Type == "OWNED_NURSERY" {
			return w.NurseryID
		}
	}
	t.Fatal("owner has no OWNED_NURSERY workspace — seed data may be missing")
	return 0
}
