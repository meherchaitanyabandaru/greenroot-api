package apitest

import (
	"net/http"
	"testing"
)

func TestDispatchList_RequiresAuth(t *testing.T) {
	resp := get(t, "/api/v1/dispatches", "")
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestDispatchList_Owner(t *testing.T) {
	token := login(t, ownerPhone)
	resp := get(t, "/api/v1/dispatches", token)
	assertStatus(t, resp, http.StatusOK)
}

func TestDispatchList_Driver(t *testing.T) {
	token := login(t, driverPhone)
	resp := get(t, "/api/v1/dispatches", token)
	// Driver sees their assigned dispatches
	assertStatus(t, resp, http.StatusOK)
}

func TestDispatchList_Buyer(t *testing.T) {
	token := login(t, buyerPhone)
	resp := get(t, "/api/v1/dispatches", token)
	// Buyer sees dispatches for their orders
	assertStatus(t, resp, http.StatusOK)
}

func TestPublicTrackLink_NotFound(t *testing.T) {
	// Non-existent UUID should return 404 — no auth required
	resp := get(t, "/api/v1/track/00000000-0000-0000-0000-000000000000", "")
	assertStatus(t, resp, http.StatusNotFound)
}

func TestPublicTrackLink_NoAuthRequired(t *testing.T) {
	// Even with a non-existent UUID, the route should not return 401
	resp := get(t, "/api/v1/track/00000000-0000-0000-0000-000000000000", "")
	if resp.StatusCode == http.StatusUnauthorized {
		t.Error("public track link should not require auth")
	}
}

func TestDispatchCreate_Buyer_Forbidden(t *testing.T) {
	token := login(t, buyerPhone)
	resp := post(t, "/api/v1/dispatches", map[string]any{"order_id": 1}, token)
	assertStatus(t, resp, http.StatusForbidden)
}

func TestDispatchGet_NotFound(t *testing.T) {
	token := login(t, ownerPhone)
	resp := get(t, "/api/v1/dispatches/999999999", token)
	assertStatus(t, resp, http.StatusNotFound)
}

func TestDispatchAccept_RequiresDriver(t *testing.T) {
	// Owner cannot accept a dispatch (driver-only action)
	token := login(t, ownerPhone)

	var dispatches struct {
		Dispatches []struct {
			DispatchID int64  `json:"dispatch_id"`
			Status     string `json:"status"`
		} `json:"dispatches"`
	}
	resp := get(t, "/api/v1/dispatches", token)
	assertStatus(t, resp, http.StatusOK)
	decode(t, resp, &dispatches)

	for _, d := range dispatches.Dispatches {
		if d.Status == "DISPATCH_CREATED" {
			acceptResp := post(t, url("/api/v1/dispatches/%d/accept", d.DispatchID), nil, token)
			// Owner should be forbidden from accepting dispatches
			assertStatus(t, acceptResp, http.StatusForbidden)
			return
		}
	}
	t.Skip("no DISPATCH_CREATED dispatch in seed data")
}
