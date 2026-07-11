package apitest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

const (
	baseURL = "http://localhost:8080"

	adminPhone   = "9000000000"
	ownerPhone   = "9100000000"
	managerPhone = "9200000000"
	buyerPhone   = "9300000000"
	driverPhone  = "9400000000"
	devOTP       = "123456"
)

type testWorkspace struct {
	Type      string `json:"type"`
	NurseryID int64  `json:"nursery_id"`
}

func getWorkspaces(t *testing.T, token string) []testWorkspace {
	t.Helper()
	resp := get(t, "/api/v1/me/workspaces", token)
	assertStatus(t, resp, http.StatusOK)

	var workspaces []testWorkspace
	decode(t, resp, &workspaces)
	return workspaces
}

func login(t *testing.T, mobile string) string {
	t.Helper()

	sendOTP(t, mobile)

	body := map[string]string{"mobile": mobile, "otp": devOTP}
	resp := post(t, "/api/v1/auth/verify-otp", body, "")

	var result struct {
		AccessToken string `json:"access_token"`
	}
	decode(t, resp, &result)

	if result.AccessToken == "" {
		t.Fatalf("login(%s): empty access_token", mobile)
	}
	return result.AccessToken
}

func sendOTP(t *testing.T, mobile string) {
	t.Helper()
	body := map[string]string{"mobile": mobile}
	resp := post(t, "/api/v1/auth/send-otp", body, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("send-otp(%s): got %d", mobile, resp.StatusCode)
	}
}

func get(t *testing.T, path, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func post(t *testing.T, path string, body any, token string) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, b)
	}
}

func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("status: got %d, want %d\nbody: %s", resp.StatusCode, want, b)
	}
}

func putReq(t *testing.T, path string, body any, token string) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, baseURL+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	return resp
}

func deleteReq(t *testing.T, path, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, baseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	return resp
}

func url(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
