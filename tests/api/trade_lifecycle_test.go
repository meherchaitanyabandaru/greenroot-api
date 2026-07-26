package apitest

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

type quotationLifecycleEnvelope struct {
	Quotation struct {
		ID               int64  `json:"id"`
		Status           string `json:"status"`
		ConvertedOrderID *int64 `json:"converted_order_id"`
	} `json:"quotation"`
}

type orderLifecycleEnvelope struct {
	Order struct {
		ID     int64  `json:"id"`
		Status string `json:"order_status"`
		Items  []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	} `json:"order"`
}

func TestQuotationLifecycle_AllRoles(t *testing.T) {
	ownerToken := login(t, ownerPhone)
	managerToken := login(t, managerPhone)
	buyerToken := login(t, buyerPhone)
	driverToken := login(t, driverPhone)
	adminToken := login(t, adminPhone)
	secondOwnerToken := login(t, secondOwnerPhone)
	nurseryID := getOwnerNurseryID(t, ownerToken)
	managerID := getManagerUserID(t, managerPhone)

	t.Run("RBAC list matrix", func(t *testing.T) {
		assertStatus(t, get(t, "/api/v1/quotations", ""), http.StatusUnauthorized)
		assertStatus(t, get(t, "/api/v1/quotations", ownerToken), http.StatusOK)
		assertStatus(t, get(t, "/api/v1/quotations", managerToken), http.StatusOK)
		assertStatus(t, get(t, "/api/v1/quotations", buyerToken), http.StatusOK)
		assertStatus(t, get(t, "/api/v1/quotations", adminToken), http.StatusOK)
		assertStatus(t, get(t, "/api/v1/quotations", driverToken), http.StatusForbidden)
	})

	qID := createCustomerQuotation(t, ownerToken, nurseryID, buyerPhone)

	t.Run("draft privacy and assignment", func(t *testing.T) {
		assertQuotationStatus(t, get(t, fmt.Sprintf("/api/v1/quotations/%d", qID), ownerToken), http.StatusOK, "CUSTOMER_DRAFT")
		assertStatus(t, get(t, fmt.Sprintf("/api/v1/quotations/%d", qID), buyerToken), http.StatusForbidden)
		assertStatus(t, get(t, fmt.Sprintf("/api/v1/quotations/%d", qID), secondOwnerToken), http.StatusForbidden)
		assertStatus(t, get(t, fmt.Sprintf("/api/v1/quotations/%d", qID), driverToken), http.StatusForbidden)
		assertStatus(t, get(t, fmt.Sprintf("/api/v1/quotations/%d", qID), adminToken), http.StatusOK)
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/assign-manager", qID), map[string]any{"manager_user_id": managerID}, managerToken), http.StatusForbidden)
		assertQuotationStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/assign-manager", qID), map[string]any{"manager_user_id": managerID}, ownerToken), http.StatusOK, "CUSTOMER_DRAFT")
	})

	t.Run("send accept and convert lifecycle", func(t *testing.T) {
		assertQuotationStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/send", qID), nil, managerToken), http.StatusOK, "CUSTOMER_SENT")
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/buyer-accept", qID), nil, ownerToken), http.StatusForbidden)
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/buyer-accept", qID), nil, secondOwnerToken), http.StatusForbidden)
		assertQuotationStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/buyer-accept", qID), nil, buyerToken), http.StatusOK, "CUSTOMER_ACCEPTED")
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/buyer-reject", qID), map[string]any{"reason": "late"}, buyerToken), http.StatusConflict)
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/convert-to-order", qID), nil, buyerToken), http.StatusForbidden)

		resp := post(t, fmt.Sprintf("/api/v1/quotations/%d/convert-to-order", qID), nil, managerToken)
		assertStatus(t, resp, http.StatusOK)
		var converted quotationLifecycleEnvelope
		decode(t, resp, &converted)
		if converted.Quotation.Status != "CONVERTED" || converted.Quotation.ConvertedOrderID == nil {
			t.Fatalf("convert result: status=%q order=%v", converted.Quotation.Status, converted.Quotation.ConvertedOrderID)
		}
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/convert-to-order", qID), nil, ownerToken), http.StatusConflict)
		assertOrderStatus(t, get(t, fmt.Sprintf("/api/v1/orders/%d", *converted.Quotation.ConvertedOrderID), buyerToken), http.StatusOK, "PENDING")
	})

	rejectedID := createCustomerQuotation(t, ownerToken, nurseryID, buyerPhone)
	assertQuotationStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/send", rejectedID), nil, ownerToken), http.StatusOK, "CUSTOMER_SENT")
	t.Run("buyer rejection is terminal", func(t *testing.T) {
		assertQuotationStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/buyer-reject", rejectedID), map[string]any{"reason": "Price too high"}, buyerToken), http.StatusOK, "CUSTOMER_REJECTED")
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/send", rejectedID), nil, ownerToken), http.StatusConflict)
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/convert-to-order", rejectedID), nil, ownerToken), http.StatusConflict)
	})

	recalledID := createCustomerQuotation(t, ownerToken, nurseryID, buyerPhone)
	assertQuotationStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/send", recalledID), nil, ownerToken), http.StatusOK, "CUSTOMER_SENT")
	t.Run("seller recall returns sent quotation to draft", func(t *testing.T) {
		assertQuotationStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/recall", recalledID), nil, managerToken), http.StatusOK, "CUSTOMER_DRAFT")
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/quotations/%d/buyer-accept", recalledID), nil, buyerToken), http.StatusConflict)
	})
}

func TestOrderLifecycle_AllRoles(t *testing.T) {
	ownerToken := login(t, ownerPhone)
	managerToken := login(t, managerPhone)
	buyerToken := login(t, buyerPhone)
	driverToken := login(t, driverPhone)
	adminToken := login(t, adminPhone)
	secondOwnerToken := login(t, secondOwnerPhone)
	nurseryID := getOwnerNurseryID(t, ownerToken)
	managerID := getManagerUserID(t, managerPhone)

	t.Run("RBAC list and create matrix", func(t *testing.T) {
		assertStatus(t, get(t, "/api/v1/orders", ""), http.StatusUnauthorized)
		assertStatus(t, get(t, "/api/v1/orders", ownerToken), http.StatusOK)
		assertStatus(t, get(t, "/api/v1/orders", managerToken), http.StatusOK)
		assertStatus(t, get(t, "/api/v1/orders", buyerToken), http.StatusOK)
		assertStatus(t, get(t, "/api/v1/orders", adminToken), http.StatusOK)
		assertStatus(t, get(t, "/api/v1/orders", driverToken), http.StatusForbidden)
		assertStatus(t, post(t, "/api/v1/orders", map[string]any{"seller_nursery_id": nurseryID}, adminToken), http.StatusForbidden)
		assertStatus(t, post(t, "/api/v1/orders", map[string]any{"seller_nursery_id": nurseryID}, driverToken), http.StatusForbidden)
	})

	orderID, itemID := createLifecycleOrder(t, ownerToken, nurseryID, buyerPhone)

	t.Run("pending access assignment and forbidden transitions", func(t *testing.T) {
		assertOrderStatus(t, get(t, fmt.Sprintf("/api/v1/orders/%d", orderID), ownerToken), http.StatusOK, "PENDING")
		assertOrderStatus(t, get(t, fmt.Sprintf("/api/v1/orders/%d", orderID), buyerToken), http.StatusOK, "PENDING")
		assertStatus(t, get(t, fmt.Sprintf("/api/v1/orders/%d", orderID), secondOwnerToken), http.StatusForbidden)
		assertStatus(t, get(t, fmt.Sprintf("/api/v1/orders/%d", orderID), driverToken), http.StatusForbidden)
		assertStatus(t, get(t, fmt.Sprintf("/api/v1/orders/%d", orderID), adminToken), http.StatusOK)
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/orders/%d/assign-manager", orderID), map[string]any{"manager_user_id": managerID}, managerToken), http.StatusForbidden)
		assertOrderStatus(t, post(t, fmt.Sprintf("/api/v1/orders/%d/assign-manager", orderID), map[string]any{"manager_user_id": managerID}, ownerToken), http.StatusOK, "PENDING")
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/orders/%d/confirm", orderID), nil, buyerToken), http.StatusForbidden)
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/orders/%d/start-loading", orderID), nil, buyerToken), http.StatusForbidden)
	})

	t.Run("confirm load and complete lifecycle", func(t *testing.T) {
		assertOrderStatus(t, post(t, fmt.Sprintf("/api/v1/orders/%d/confirm", orderID), nil, managerToken), http.StatusOK, "CONFIRMED")
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/orders/%d/cancel", orderID), map[string]any{"reason": "buyer changed mind"}, buyerToken), http.StatusForbidden)
		assertOrderStatus(t, post(t, fmt.Sprintf("/api/v1/orders/%d/start-loading", orderID), nil, managerToken), http.StatusOK, "LOADING")
		assertStatus(t, putReq(t, fmt.Sprintf("/api/v1/orders/%d/items/%d/loaded-quantity", orderID, itemID), map[string]any{"loaded_quantity": 2}, buyerToken), http.StatusForbidden)
		assertStatus(t, putReq(t, fmt.Sprintf("/api/v1/orders/%d/items/%d/loaded-quantity", orderID, itemID), map[string]any{"loaded_quantity": 2}, managerToken), http.StatusOK)
		assertOrderStatus(t, post(t, fmt.Sprintf("/api/v1/orders/%d/complete-loading", orderID), nil, managerToken), http.StatusOK, "LOADED")
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/orders/%d/cancel", orderID), map[string]any{"reason": "too late"}, ownerToken), http.StatusConflict)
		assertStatus(t, putReq(t, fmt.Sprintf("/api/v1/orders/%d/status", orderID), map[string]any{"order_status": "COMPLETED"}, driverToken), http.StatusForbidden)
		assertOrderStatus(t, putReq(t, fmt.Sprintf("/api/v1/orders/%d/status", orderID), map[string]any{"order_status": "COMPLETED"}, ownerToken), http.StatusOK, "COMPLETED")
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/orders/%d/cancel", orderID), map[string]any{"reason": "terminal"}, adminToken), http.StatusConflict)
	})

	buyerOrderID, _ := createLifecycleOrder(t, buyerToken, nurseryID, "")
	t.Run("buyer may cancel only own pending order", func(t *testing.T) {
		assertOrderStatus(t, post(t, fmt.Sprintf("/api/v1/orders/%d/cancel", buyerOrderID), map[string]any{"reason": "No longer needed"}, buyerToken), http.StatusOK, "CANCELLED")
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/orders/%d/cancel", buyerOrderID), map[string]any{"reason": "again"}, buyerToken), http.StatusConflict)
	})
}

func createCustomerQuotation(t *testing.T, token string, nurseryID int64, recipientMobile string) int64 {
	t.Helper()
	resp := post(t, "/api/v1/quotations", map[string]any{
		"quotation_type":   "CUSTOMER",
		"nursery_id":       nurseryID,
		"recipient_name":   "Lifecycle Buyer",
		"recipient_mobile": recipientMobile,
		"notes":            "full lifecycle " + time.Now().Format(time.RFC3339Nano),
		"items":            []map[string]any{{"plant_id": 1, "quantity": 2, "unit_price": 100, "total_price": 200}},
	}, token)
	assertStatus(t, resp, http.StatusCreated)
	var result quotationLifecycleEnvelope
	decode(t, resp, &result)
	if result.Quotation.ID == 0 || result.Quotation.Status != "CUSTOMER_DRAFT" {
		t.Fatalf("create quotation: id=%d status=%q", result.Quotation.ID, result.Quotation.Status)
	}
	return result.Quotation.ID
}

func createLifecycleOrder(t *testing.T, token string, nurseryID int64, buyerMobile string) (int64, int64) {
	t.Helper()
	body := map[string]any{
		"seller_nursery_id": nurseryID,
		"buyer_name":        "Lifecycle Buyer",
		"notes":             "full lifecycle " + time.Now().Format(time.RFC3339Nano),
		"items":             []map[string]any{{"plant_id": 1, "quantity": 2, "unit_price": 100, "total_price": 200}},
		"delivery": map[string]any{
			"contact_name":   "Lifecycle Buyer",
			"contact_mobile": buyerPhone,
			"address_line1":  "123 Lifecycle Road",
			"city":           "Hyderabad",
			"state":          "Telangana",
			"postal_code":    "500001",
			"country":        "India",
		},
	}
	if buyerMobile != "" {
		body["buyer_mobile"] = buyerMobile
	}
	resp := post(t, "/api/v1/orders", body, token)
	assertStatus(t, resp, http.StatusCreated)
	var result orderLifecycleEnvelope
	decode(t, resp, &result)
	if result.Order.ID == 0 || result.Order.Status != "PENDING" || len(result.Order.Items) == 0 {
		t.Fatalf("create order: id=%d status=%q items=%d", result.Order.ID, result.Order.Status, len(result.Order.Items))
	}
	return result.Order.ID, result.Order.Items[0].ID
}

func assertQuotationStatus(t *testing.T, resp *http.Response, wantHTTP int, wantStatus string) {
	t.Helper()
	assertStatus(t, resp, wantHTTP)
	if wantHTTP < 200 || wantHTTP >= 300 {
		return
	}
	var result quotationLifecycleEnvelope
	decode(t, resp, &result)
	if result.Quotation.Status != wantStatus {
		t.Fatalf("quotation status: got %q, want %q", result.Quotation.Status, wantStatus)
	}
}

func assertOrderStatus(t *testing.T, resp *http.Response, wantHTTP int, wantStatus string) {
	t.Helper()
	assertStatus(t, resp, wantHTTP)
	if wantHTTP < 200 || wantHTTP >= 300 {
		return
	}
	var result orderLifecycleEnvelope
	decode(t, resp, &result)
	if result.Order.Status != wantStatus {
		t.Fatalf("order status: got %q, want %q", result.Order.Status, wantStatus)
	}
}
