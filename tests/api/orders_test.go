package apitest

import (
	"net/http"
	"testing"
)

func TestOrdersList_RequiresAuth(t *testing.T) {
	resp := get(t, "/api/v1/orders", "")
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestOrdersList_Owner(t *testing.T) {
	token := login(t, ownerPhone)
	resp := get(t, "/api/v1/orders", token)
	assertStatus(t, resp, http.StatusOK)
}

func TestOrdersList_Manager(t *testing.T) {
	token := login(t, managerPhone)
	resp := get(t, "/api/v1/orders", token)
	assertStatus(t, resp, http.StatusOK)
}

func TestOrdersList_Buyer(t *testing.T) {
	token := login(t, buyerPhone)
	resp := get(t, "/api/v1/orders", token)
	assertStatus(t, resp, http.StatusOK)
}

func TestOrdersList_Driver_Forbidden(t *testing.T) {
	token := login(t, driverPhone)
	resp := get(t, "/api/v1/orders", token)
	// Drivers cannot access orders
	assertStatus(t, resp, http.StatusForbidden)
}

func TestOrderCreate_Owner(t *testing.T) {
	token := login(t, ownerPhone)

	var nurseryID int64
	for _, w := range getWorkspaces(t, token) {
		if w.Type == "OWNED_NURSERY" {
			nurseryID = w.NurseryID
			break
		}
	}
	if nurseryID == 0 {
		t.Skip("no owned nursery — seed data required")
	}

	body := map[string]any{
		"nursery_id":   nurseryID,
		"buyer_name":   "Test Walk-in",
		"buyer_mobile": "9999999999",
		"notes":        "integration test order",
	}
	resp := post(t, "/api/v1/orders", body, token)
	assertStatus(t, resp, http.StatusCreated)

	var order struct {
		OrderID int64  `json:"order_id"`
		Status  string `json:"status"`
	}
	decode(t, resp, &order)

	if order.Status != "PENDING" {
		t.Errorf("new order status: got %q, want %q", order.Status, "PENDING")
	}
}

func TestOrderCreate_Driver_Forbidden(t *testing.T) {
	token := login(t, driverPhone)
	resp := post(t, "/api/v1/orders", map[string]any{"nursery_id": 1}, token)
	assertStatus(t, resp, http.StatusForbidden)
}

func TestOrderGet_NotFound(t *testing.T) {
	token := login(t, ownerPhone)
	resp := get(t, "/api/v1/orders/999999999", token)
	assertStatus(t, resp, http.StatusNotFound)
}

func TestOrderStateTransition_CancelLoaded(t *testing.T) {
	// Attempting to cancel a LOADED order should return 409
	// This test requires a LOADED order in the seed data.
	// Skip if no such order exists.
	token := login(t, ownerPhone)

	var orders struct {
		Orders []struct {
			OrderID int64  `json:"order_id"`
			Status  string `json:"status"`
		} `json:"orders"`
	}
	resp := get(t, "/api/v1/orders", token)
	assertStatus(t, resp, http.StatusOK)
	decode(t, resp, &orders)

	for _, o := range orders.Orders {
		if o.Status == "LOADED" {
			cancelResp := post(t, url("/api/v1/orders/%d/cancel", o.OrderID), nil, token)
			assertStatus(t, cancelResp, http.StatusConflict)
			return
		}
	}
	t.Skip("no LOADED order in seed data")
}
