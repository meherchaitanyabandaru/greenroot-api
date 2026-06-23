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
	token   string
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
	var mobile string
	var timeout time.Duration
	flag.StringVar(&baseURL, "base-url", env("SMOKE_BASE_URL", "http://127.0.0.1:8080"), "API base URL")
	flag.StringVar(&mobile, "mobile", env("SMOKE_MOBILE", "9000000777"), "mobile number used for OTP login")
	flag.DurationVar(&timeout, "timeout", 20*time.Second, "smoke test timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c := &client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 5 * time.Second},
	}
	results := make([]result, 0, 32)
	record := func(name string, err error) {
		if err != nil {
			results = append(results, result{Name: name, Status: "FAIL", Detail: err.Error()})
			return
		}
		results = append(results, result{Name: name, Status: "PASS"})
	}
	skip := func(name, detail string) {
		results = append(results, result{Name: name, Status: "SKIP", Detail: detail})
	}

	record("health", c.expect(ctx, http.MethodGet, "/health", nil, "", http.StatusOK))
	record("healthz", c.expect(ctx, http.MethodGet, "/healthz", nil, "", http.StatusOK))
	record("readyz", c.expect(ctx, http.MethodGet, "/readyz", nil, "", http.StatusOK))
	record("openapi", c.expectContains(ctx, "/openapi.yaml", []string{
		"/api/v1/auth/verify-otp",
		"/api/v1/plants",
		"/api/v1/admin/dashboard",
		"/api/v1/attachments",
		"/api/v1/tracking",
	}))

	record("auth/send-otp", c.expect(ctx, http.MethodPost, "/api/v1/auth/send-otp", map[string]any{"mobile": mobile}, "", http.StatusOK))
	login, err := c.login(ctx, mobile)
	record("auth/verify-otp", err)
	if err != nil {
		printResults(results)
		os.Exit(1)
	}
	c.token = login.AccessToken
	record("auth/me", c.expect(ctx, http.MethodGet, "/api/v1/auth/me", nil, c.token, http.StatusOK))
	record("auth/refresh-token", c.expect(ctx, http.MethodPost, "/api/v1/auth/refresh-token", map[string]any{"refresh_token": login.RefreshToken}, "", http.StatusOK))

	record("public/plants", c.expect(ctx, http.MethodGet, "/api/v1/plants?page=1&per_page=5", nil, "", http.StatusOK))
	record("public/plant-categories", c.expect(ctx, http.MethodGet, "/api/v1/plants/categories", nil, "", http.StatusOK))
	record("public/nurseries", c.expect(ctx, http.MethodGet, "/api/v1/nurseries?page=1&per_page=5", nil, "", http.StatusOK))
	record("public/subscription-plans", c.expect(ctx, http.MethodGet, "/api/v1/subscription-plans", nil, "", http.StatusOK))

	record("auth-required/plants-create", c.expect(ctx, http.MethodPost, "/api/v1/plants", map[string]any{"scientific_name": "Smoke Unauthorized"}, "", http.StatusUnauthorized))
	record("users/me", c.expect(ctx, http.MethodGet, "/api/v1/users/me", nil, c.token, http.StatusOK))
	record("users/me/roles", c.expect(ctx, http.MethodGet, fmt.Sprintf("/api/v1/users/%d/roles", login.User.ID), nil, c.token, http.StatusOK, http.StatusForbidden))
	record("notifications/list", c.expect(ctx, http.MethodGet, "/api/v1/notifications?page=1&per_page=5", nil, c.token, http.StatusOK))
	record("notifications/devices/list", c.expect(ctx, http.MethodGet, "/api/v1/notifications/devices", nil, c.token, http.StatusOK))
	record("notifications/devices/upsert", c.expect(ctx, http.MethodPost, "/api/v1/notifications/devices", map[string]any{
		"fcm_token":          "smoke-test-token",
		"device_type":        "test",
		"platform":           "codex",
		"app_version":        "smoke",
		"device_id_external": "greenroot-smoke",
	}, c.token, http.StatusCreated, http.StatusOK))

	record("inventory/list", c.expect(ctx, http.MethodGet, "/api/v1/inventory?page=1&per_page=5", nil, c.token, http.StatusOK))
	record("plant-requests/list", c.expect(ctx, http.MethodGet, "/api/v1/plant-requests?page=1&per_page=5", nil, c.token, http.StatusOK))
	record("orders/list", c.expect(ctx, http.MethodGet, "/api/v1/orders?page=1&per_page=5", nil, c.token, http.StatusOK))
	record("payments/list", c.expect(ctx, http.MethodGet, "/api/v1/payments?page=1&per_page=5", nil, c.token, http.StatusOK))
	record("subscriptions/me", c.expect(ctx, http.MethodGet, "/api/v1/subscriptions/me", nil, c.token, http.StatusOK, http.StatusNotFound))
	record("dispatches/list", c.expect(ctx, http.MethodGet, "/api/v1/dispatches?page=1&per_page=5", nil, c.token, http.StatusOK, http.StatusForbidden))
	record("vehicles/list", c.expect(ctx, http.MethodGet, "/api/v1/vehicles?page=1&per_page=5", nil, c.token, http.StatusOK, http.StatusForbidden))
	record("drivers/list", c.expect(ctx, http.MethodGet, "/api/v1/drivers?page=1&per_page=5", nil, c.token, http.StatusOK, http.StatusForbidden))
	record("attachments/list", c.expect(ctx, http.MethodGet, "/api/v1/attachments?page=1&per_page=5", nil, c.token, http.StatusOK))

	if hasRole(login.User.Roles, "ADMIN") {
		record("admin/dashboard", c.expect(ctx, http.MethodGet, "/api/v1/admin/dashboard", nil, c.token, http.StatusOK))
		record("audit/list", c.expect(ctx, http.MethodGet, "/api/v1/audit-logs?page=1&per_page=5", nil, c.token, http.StatusOK))
		record("notifications/templates", c.expect(ctx, http.MethodGet, "/api/v1/notifications/templates", nil, c.token, http.StatusOK))
	} else {
		record("admin/dashboard-forbidden", c.expect(ctx, http.MethodGet, "/api/v1/admin/dashboard", nil, c.token, http.StatusForbidden))
		record("audit/list-forbidden", c.expect(ctx, http.MethodGet, "/api/v1/audit-logs?page=1&per_page=5", nil, c.token, http.StatusForbidden))
		record("notifications/templates-forbidden", c.expect(ctx, http.MethodGet, "/api/v1/notifications/templates", nil, c.token, http.StatusForbidden))
		skip("admin-positive-role-checks", "login user has roles: "+strings.Join(login.User.Roles, ","))
	}

	record("auth/logout", c.expect(ctx, http.MethodPost, "/api/v1/auth/logout", map[string]any{"refresh_token": login.RefreshToken}, c.token, http.StatusOK, http.StatusUnauthorized, http.StatusBadRequest))

	printResults(results)
	if failed(results) {
		os.Exit(1)
	}
}

func (c *client) login(ctx context.Context, mobile string) (authResponse, error) {
	var out authResponse
	res, body, err := c.request(ctx, http.MethodPost, "/api/v1/auth/verify-otp", map[string]any{
		"mobile":      mobile,
		"otp":         "123456",
		"device_type": "smoke",
		"os_name":     "codex",
		"app_version": "smoke",
	}, "")
	if err != nil {
		return out, err
	}
	if res.StatusCode != http.StatusOK {
		return out, fmt.Errorf("expected 200, got %d: %s", res.StatusCode, trim(body))
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	if out.AccessToken == "" || out.RefreshToken == "" || out.User.ID == 0 {
		return out, errors.New("auth response missing token or user")
	}
	return out, nil
}

func (c *client) expect(ctx context.Context, method string, path string, body any, token string, statuses ...int) error {
	res, data, err := c.request(ctx, method, path, body, token)
	if err != nil {
		return err
	}
	for _, status := range statuses {
		if res.StatusCode == status {
			return nil
		}
	}
	return fmt.Errorf("expected %s, got %d: %s", joinStatuses(statuses), res.StatusCode, trim(data))
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

func (c *client) request(ctx context.Context, method string, path string, body any, token string) (*http.Response, []byte, error) {
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

func hasRole(roles []string, want string) bool {
	for _, role := range roles {
		if strings.EqualFold(role, want) {
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
	if len(text) > 240 {
		return text[:240] + "..."
	}
	return text
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
