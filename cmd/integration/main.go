package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type client struct {
	baseURL string
	http    *http.Client
}

type actor struct {
	Name         string
	Mobile       string
	AccessToken  string
	RefreshToken string
	UserID       int64
	Roles        []string
}

type result struct {
	Name   string
	Status string
	Detail string
}

type authResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	User         struct {
		ID    int64    `json:"id"`
		Roles []string `json:"roles"`
	} `json:"user"`
}

func main() {
	var baseURL string
	var timeout time.Duration
	flag.StringVar(&baseURL, "base-url", env("INTEGRATION_BASE_URL", "http://127.0.0.1:18096"), "API base URL")
	flag.DurationVar(&timeout, "timeout", 45*time.Second, "integration test timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c := &client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 8 * time.Second}}
	results := make([]result, 0, 80)
	record := func(name string, err error) {
		if err != nil {
			results = append(results, result{Name: name, Status: "FAIL", Detail: err.Error()})
			return
		}
		results = append(results, result{Name: name, Status: "PASS"})
	}

	record("health", c.expect(ctx, http.MethodGet, "/health", nil, "", http.StatusOK))
	record("openapi", c.expectContains(ctx, "/openapi.yaml", []string{"/api/v1/auth/verify-otp", "/api/v1/orders", "/api/v1/tracking"}))

	admin, err := c.login(ctx, "admin", env("INTEGRATION_ADMIN_MOBILE", "9100000001"))
	record("auth/admin-login", err)
	buyer, err := c.login(ctx, "buyer", env("INTEGRATION_BUYER_MOBILE", "9100000002"))
	record("auth/buyer-login", err)
	nursery, err := c.login(ctx, "nursery", env("INTEGRATION_NURSERY_MOBILE", "9100000003"))
	record("auth/nursery-login", err)
	driver, err := c.login(ctx, "driver", env("INTEGRATION_DRIVER_MOBILE", "9100000004"))
	record("auth/driver-login", err)
	if failed(results) {
		printResults(results)
		os.Exit(1)
	}

	record("rbac/buyer-admin-dashboard-forbidden", c.expect(ctx, http.MethodGet, "/api/v1/admin/dashboard", nil, buyer.AccessToken, http.StatusForbidden))
	record("rbac/buyer-audit-forbidden", c.expect(ctx, http.MethodGet, "/api/v1/audit-logs", nil, buyer.AccessToken, http.StatusForbidden))
	record("rbac/buyer-create-plant-forbidden", c.expect(ctx, http.MethodPost, "/api/v1/plants", map[string]any{"scientific_name": "Forbidden Plant"}, buyer.AccessToken, http.StatusForbidden))
	record("rbac/nursery-vehicles-forbidden", c.expect(ctx, http.MethodGet, "/api/v1/vehicles", nil, nursery.AccessToken, http.StatusForbidden))
	record("rbac/driver-admin-forbidden", c.expect(ctx, http.MethodGet, "/api/v1/admin/dashboard", nil, driver.AccessToken, http.StatusForbidden))
	record("rbac/admin-dashboard", c.expect(ctx, http.MethodGet, "/api/v1/admin/dashboard", nil, admin.AccessToken, http.StatusOK))

	plantID := int64(1)
	plantBody := map[string]any{
		"scientific_name":     unique("Integration Plant"),
		"common_name":         "Integration Plant",
		"english_description": "Integration-created plant",
		"plant_type":          "TREE",
		"light_requirement":   "FULL_SUN",
		"water_requirement":   "MEDIUM",
		"category_ids":        []int64{1},
		"care_guide":          map[string]any{"sunlight": "Full sun", "watering": "Weekly"},
	}
	if data, err := c.expectJSON(ctx, http.MethodPost, "/api/v1/plants", plantBody, admin.AccessToken, http.StatusCreated); err == nil {
		plantID = nestedInt(data, "plant", "id")
		record("plants/create-admin", nil)
	} else {
		record("plants/create-admin", err)
	}
	record("plants/list-public", c.expect(ctx, http.MethodGet, "/api/v1/plants?page=1&per_page=5", nil, "", http.StatusOK))
	record("plants/detail-public", c.expect(ctx, http.MethodGet, fmt.Sprintf("/api/v1/plants/%d", plantID), nil, "", http.StatusOK))
	record("plants/update-admin", c.expect(ctx, http.MethodPut, fmt.Sprintf("/api/v1/plants/%d", plantID), plantBody, admin.AccessToken, http.StatusOK))
	record("plants/image-admin", c.expect(ctx, http.MethodPost, fmt.Sprintf("/api/v1/plants/%d/images", plantID), map[string]any{"image_url": "https://example.com/integration-plant.jpg", "alt_text": "Integration plant", "display_order": 1, "is_primary": true}, admin.AccessToken, http.StatusCreated))
	record("plants/care-guide", c.expect(ctx, http.MethodGet, fmt.Sprintf("/api/v1/plants/%d/care-guide", plantID), nil, "", http.StatusOK, http.StatusNotFound))

	nurseryID := int64(1)
	nurseryBody := map[string]any{"code": unique("INT-NUR"), "name": unique("Integration Nursery"), "mobile": "919999999999", "email": unique("integration") + "@greenroot.test", "status": "ACTIVE"}
	if data, err := c.expectJSON(ctx, http.MethodPost, "/api/v1/nurseries", nurseryBody, admin.AccessToken, http.StatusCreated); err == nil {
		nurseryID = nestedInt(data, "nursery", "id")
		record("nurseries/create-admin", nil)
	} else {
		record("nurseries/create-admin", err)
	}
	record("nurseries/list-public", c.expect(ctx, http.MethodGet, "/api/v1/nurseries?page=1&per_page=5", nil, "", http.StatusOK))
	record("nurseries/update-admin", c.expect(ctx, http.MethodPut, fmt.Sprintf("/api/v1/nurseries/%d", nurseryID), nurseryBody, admin.AccessToken, http.StatusOK))
	record("nurseries/address-create", c.expect(ctx, http.MethodPost, fmt.Sprintf("/api/v1/nurseries/%d/addresses", nurseryID), map[string]any{"address_line1": "Integration Street", "city": "Hyderabad", "state": "Telangana", "country": "India", "postal_code": "500001", "is_primary": true}, admin.AccessToken, http.StatusCreated))
	record("nurseries/users-list", c.expect(ctx, http.MethodGet, "/api/v1/nurseries/1/users", nil, nursery.AccessToken, http.StatusOK))

	inventoryID := int64(0)
	inventoryBody := map[string]any{"nursery_id": 1, "plant_id": 1, "size_id": 1, "available_quantity": 20, "inventory_status": "AVAILABLE"}
	if data, err := c.expectJSON(ctx, http.MethodPost, "/api/v1/inventory", inventoryBody, nursery.AccessToken, http.StatusCreated, http.StatusOK); err == nil {
		inventoryID = nestedInt(data, "inventory", "id")
		record("inventory/create-nursery", nil)
	} else {
		record("inventory/create-nursery", err)
	}
	record("inventory/list", c.expect(ctx, http.MethodGet, "/api/v1/inventory?page=1&per_page=5", nil, admin.AccessToken, http.StatusOK))
	if inventoryID > 0 {
		inventoryBody["available_quantity"] = 25
		record("inventory/update-nursery", c.expect(ctx, http.MethodPut, fmt.Sprintf("/api/v1/inventory/%d", inventoryID), inventoryBody, nursery.AccessToken, http.StatusOK))
	}

	requestID := int64(0)
	requestBody := map[string]any{"requesting_nursery_id": nurseryID, "plant_id": 1, "size_id": 1, "quantity_required": 2, "radius_km": 25, "status": "OPEN"}
	if data, err := c.expectJSON(ctx, http.MethodPost, "/api/v1/plant-requests", requestBody, admin.AccessToken, http.StatusCreated); err == nil {
		requestID = nestedInt(data, "request", "id")
		record("requests/create-nursery", nil)
	} else {
		record("requests/create-nursery", err)
	}
	record("requests/list-admin", c.expect(ctx, http.MethodGet, "/api/v1/plant-requests?page=1&per_page=5", nil, admin.AccessToken, http.StatusOK))
	if requestID > 0 {
		record("requests/respond-nursery", c.expect(ctx, http.MethodPost, fmt.Sprintf("/api/v1/plant-requests/%d/responses", requestID), map[string]any{"supplier_nursery_id": 1, "available_quantity": 2, "status": "AVAILABLE"}, nursery.AccessToken, http.StatusCreated))
	}

	orderID := int64(0)
	orderBody := map[string]any{"buyer_user_id": buyer.UserID, "seller_nursery_id": 1, "order_status": "PENDING", "items": []map[string]any{{"plant_id": 1, "size_id": 1, "quantity": 2, "unit_price": 100, "total_price": 200}}}
	if data, err := c.expectJSON(ctx, http.MethodPost, "/api/v1/orders", orderBody, buyer.AccessToken, http.StatusCreated); err == nil {
		orderID = nestedInt(data, "order", "id")
		record("orders/create-buyer", nil)
	} else {
		record("orders/create-buyer", err)
	}
	record("orders/list-buyer", c.expect(ctx, http.MethodGet, "/api/v1/orders?page=1&per_page=5", nil, buyer.AccessToken, http.StatusOK))
	if orderID > 0 {
		record("orders/status-nursery", c.expect(ctx, http.MethodPut, fmt.Sprintf("/api/v1/orders/%d/status", orderID), map[string]any{"order_status": "CONFIRMED"}, nursery.AccessToken, http.StatusOK))
		record("payments/manual-order", c.expect(ctx, http.MethodPost, "/api/v1/payments/manual", map[string]any{"payment_for": "ORDER", "order_id": orderID, "amount": 200, "payment_method": "UPI", "payment_status": "SUCCESS", "transaction_reference": unique("UPI")}, buyer.AccessToken, http.StatusCreated))
	}

	subscriptionID := int64(0)
	if data, err := c.expectJSON(ctx, http.MethodPost, "/api/v1/subscriptions", map[string]any{"user_id": buyer.UserID, "plan_id": 1, "billing_cycle": "MONTHLY", "auto_renew": false, "payment_method": "UPI"}, buyer.AccessToken, http.StatusCreated, http.StatusConflict); err == nil {
		subscriptionID = nestedInt(data, "subscription", "id")
		record("subscriptions/create-buyer", nil)
	} else {
		record("subscriptions/create-buyer", err)
	}
	record("subscriptions/me", c.expect(ctx, http.MethodGet, "/api/v1/subscriptions/me", nil, buyer.AccessToken, http.StatusOK))
	if subscriptionID > 0 {
		record("subscriptions/cancel-buyer", c.expect(ctx, http.MethodPost, fmt.Sprintf("/api/v1/subscriptions/%d/cancel", subscriptionID), map[string]any{"cancel_immediately": true, "reason": "integration"}, buyer.AccessToken, http.StatusOK))
	}

	vehicleID := int64(0)
	if data, err := c.expectJSON(ctx, http.MethodPost, "/api/v1/vehicles", map[string]any{"vehicle_number": unique("TS-INT"), "vehicle_type": "TRUCK", "capacity_kg": 1000, "owner_name": "GreenRoot", "mobile": "919999999998", "status": "ACTIVE"}, admin.AccessToken, http.StatusCreated); err == nil {
		vehicleID = nestedInt(data, "vehicle", "id")
		record("vehicles/create-admin", nil)
	} else {
		record("vehicles/create-admin", err)
	}
	driverID := int64(0)
	if data, err := c.expectJSON(ctx, http.MethodPost, "/api/v1/drivers", map[string]any{"user_id": driver.UserID, "license_number": unique("DL"), "license_expiry_date": "2032-12-31", "emergency_contact": "919999999997", "status": "ACTIVE"}, admin.AccessToken, http.StatusCreated); err == nil {
		driverID = nestedInt(data, "driver", "id")
		record("drivers/create-admin", nil)
	} else {
		record("drivers/create-admin", err)
	}
	if driverID > 0 {
		record("drivers/location-driver", c.expect(ctx, http.MethodPost, fmt.Sprintf("/api/v1/drivers/%d/location", driverID), map[string]any{"latitude": 17.385, "longitude": 78.4867}, driver.AccessToken, http.StatusCreated))
	}

	dispatchID := int64(0)
	if orderID > 0 {
		body := map[string]any{"order_id": orderID, "vehicle_id": nullableID(vehicleID), "driver_id": nullableID(driverID), "destination_address": "Integration destination", "items": []map[string]any{}}
		if data, err := c.expectJSON(ctx, http.MethodPost, "/api/v1/dispatches", body, nursery.AccessToken, http.StatusCreated); err == nil {
			dispatchID = nestedInt(data, "dispatch", "id")
			record("dispatches/create-nursery", nil)
		} else {
			record("dispatches/create-nursery", err)
		}
	}
	record("dispatches/list-admin", c.expect(ctx, http.MethodGet, "/api/v1/dispatches?page=1&per_page=5", nil, admin.AccessToken, http.StatusOK))
	trackingBody := map[string]any{"latitude": 17.385, "longitude": 78.4867, "notes": "integration"}
	if vehicleID > 0 {
		trackingBody["vehicle_id"] = vehicleID
	}
	if driverID > 0 {
		trackingBody["driver_id"] = driverID
	}
	if dispatchID > 0 {
		trackingBody["dispatch_id"] = dispatchID
	}
	record("tracking/create-driver", c.expect(ctx, http.MethodPost, "/api/v1/tracking", trackingBody, driver.AccessToken, http.StatusCreated))
	if vehicleID > 0 {
		record("tracking/latest-vehicle", c.expect(ctx, http.MethodGet, fmt.Sprintf("/api/v1/vehicles/%d/tracking/latest", vehicleID), nil, admin.AccessToken, http.StatusOK))
	}

	attachmentID := int64(0)
	if data, err := c.expectJSON(ctx, http.MethodPost, "/api/v1/attachments", map[string]any{"entity_type": "ORDER", "entity_id": max(orderID, 1), "file_name": "integration.txt", "file_url": "https://example.com/integration.txt", "file_type": "text/plain", "file_size": 128}, nursery.AccessToken, http.StatusCreated); err == nil {
		attachmentID = nestedInt(data, "attachment", "id")
		record("attachments/create", nil)
	} else {
		record("attachments/create", err)
	}
	record("attachments/list", c.expect(ctx, http.MethodGet, "/api/v1/attachments?page=1&per_page=5", nil, buyer.AccessToken, http.StatusOK))
	if attachmentID > 0 {
		record("attachments/delete-admin", c.expect(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/attachments/%d", attachmentID), nil, admin.AccessToken, http.StatusOK))
	}

	record("notifications/device-buyer", c.expect(ctx, http.MethodPost, "/api/v1/notifications/devices", map[string]any{"fcm_token": unique("integration-token"), "device_type": "test", "platform": "integration"}, buyer.AccessToken, http.StatusCreated, http.StatusOK))
	record("notifications/template-admin", c.expect(ctx, http.MethodPost, "/api/v1/notifications/templates", map[string]any{"template_code": unique("INT_TEMPLATE"), "template_name": "Integration Template", "channel": "IN_APP", "subject": "Integration", "message_template": "Integration", "is_active": true}, admin.AccessToken, http.StatusCreated))
	record("notifications/create-admin", c.expect(ctx, http.MethodPost, "/api/v1/notifications", map[string]any{"user_id": buyer.UserID, "notification_type": "SYSTEM", "title": "Integration", "message": "Integration notification", "channel": "IN_APP", "data": map[string]any{"source": "integration"}}, admin.AccessToken, http.StatusCreated))
	record("notifications/list-buyer", c.expect(ctx, http.MethodGet, "/api/v1/notifications?page=1&per_page=5", nil, buyer.AccessToken, http.StatusOK))
	record("audit/admin-list", c.expect(ctx, http.MethodGet, "/api/v1/audit-logs?page=1&per_page=5", nil, admin.AccessToken, http.StatusOK))

	record("auth/logout-admin", c.expect(ctx, http.MethodPost, "/api/v1/auth/logout", map[string]any{"refresh_token": admin.RefreshToken}, admin.AccessToken, http.StatusOK, http.StatusUnauthorized))
	record("auth/logout-buyer", c.expect(ctx, http.MethodPost, "/api/v1/auth/logout", map[string]any{"refresh_token": buyer.RefreshToken}, buyer.AccessToken, http.StatusOK, http.StatusUnauthorized))

	printResults(results)
	if failed(results) {
		os.Exit(1)
	}
}

func (c *client) login(ctx context.Context, name, mobile string) (actor, error) {
	a := actor{Name: name, Mobile: mobile}
	res, body, err := c.request(ctx, http.MethodPost, "/api/v1/auth/verify-otp", map[string]any{"mobile": mobile, "otp": "123456", "device_type": "integration", "os_name": "codex", "app_version": "integration"}, "")
	if err != nil {
		return a, err
	}
	if res.StatusCode != http.StatusOK {
		return a, fmt.Errorf("expected 200, got %d: %s", res.StatusCode, trim(body))
	}
	var out authResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return a, err
	}
	if out.AccessToken == "" || out.RefreshToken == "" || out.User.ID == 0 {
		return a, errors.New("auth response missing token or user")
	}
	a.AccessToken = out.AccessToken
	a.RefreshToken = out.RefreshToken
	a.UserID = out.User.ID
	a.Roles = out.User.Roles
	return a, nil
}

func (c *client) expect(ctx context.Context, method, path string, body any, token string, statuses ...int) error {
	_, err := c.expectJSON(ctx, method, path, body, token, statuses...)
	return err
}

func (c *client) expectJSON(ctx context.Context, method, path string, body any, token string, statuses ...int) (map[string]any, error) {
	res, data, err := c.request(ctx, method, path, body, token)
	if err != nil {
		return nil, err
	}
	for _, status := range statuses {
		if res.StatusCode == status {
			var out map[string]any
			_ = json.Unmarshal(data, &out)
			return out, nil
		}
	}
	return nil, fmt.Errorf("%s %s expected %s, got %d: %s", method, path, joinStatuses(statuses), res.StatusCode, trim(data))
}

func (c *client) expectContains(ctx context.Context, path string, values []string) error {
	res, data, err := c.request(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("expected 200, got %d", res.StatusCode)
	}
	text := string(data)
	missing := make([]string, 0)
	for _, value := range values {
		if !strings.Contains(text, value) {
			missing = append(missing, value)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("openapi missing %s", strings.Join(missing, ", "))
	}
	return nil
}

func (c *client) request(ctx context.Context, method, path string, body any, token string) (*http.Response, []byte, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, nil, err
	}
	return res, data, nil
}

func nestedInt(data map[string]any, keys ...string) int64 {
	var current any = data
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return 0
		}
		current = m[key]
	}
	switch v := current.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}

func printResults(results []result) {
	width := 0
	for _, r := range results {
		if len(r.Name) > width {
			width = len(r.Name)
		}
	}
	for _, r := range results {
		if r.Detail == "" {
			fmt.Printf("%-*s  %s\n", width, r.Name, r.Status)
			continue
		}
		fmt.Printf("%-*s  %s  %s\n", width, r.Name, r.Status, r.Detail)
	}
}

func failed(results []result) bool {
	for _, r := range results {
		if r.Status == "FAIL" {
			return true
		}
	}
	return false
}

func joinStatuses(statuses []int) string {
	parts := make([]string, 0, len(statuses))
	for _, status := range statuses {
		parts = append(parts, fmt.Sprint(status))
	}
	sort.Strings(parts)
	return strings.Join(parts, "/")
}

func trim(data []byte) string {
	text := strings.TrimSpace(string(data))
	if len(text) > 260 {
		return text[:260] + "..."
	}
	return text
}

func unique(prefix string) string {
	clean := strings.NewReplacer(" ", "-", "_", "-").Replace(strings.ToUpper(prefix))
	return fmt.Sprintf("%s-%d", clean, time.Now().UnixNano())
}

func nullableID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
