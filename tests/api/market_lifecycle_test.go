package apitest

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

type marketAdEnvelope struct {
	Ad struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
		Title  string `json:"title"`
	} `json:"ad"`
}

type marketEnquiryEnvelope struct {
	Enquiry struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	} `json:"enquiry"`
}

func TestMarketLifecycle_AllRoles(t *testing.T) {
	ownerToken := login(t, ownerPhone)
	managerToken := login(t, managerPhone)
	buyerToken := login(t, buyerPhone)
	driverToken := login(t, driverPhone)
	adminToken := login(t, adminPhone)
	secondOwnerToken := login(t, secondOwnerPhone)

	t.Run("RBAC browse matrix", func(t *testing.T) {
		assertStatus(t, get(t, "/api/v1/market/ads", ""), http.StatusUnauthorized)
		assertStatus(t, get(t, "/api/v1/market/ads", ownerToken), http.StatusOK)
		assertStatus(t, get(t, "/api/v1/market/ads", managerToken), http.StatusOK)
		assertStatus(t, get(t, "/api/v1/market/ads", adminToken), http.StatusOK)
		assertStatus(t, get(t, "/api/v1/market/ads", buyerToken), http.StatusForbidden)
		assertStatus(t, get(t, "/api/v1/market/ads", driverToken), http.StatusForbidden)
	})

	adID := createMarketAd(t, ownerToken, "Lifecycle ficus "+time.Now().Format("150405.000"))

	t.Run("draft ownership and forbidden actions", func(t *testing.T) {
		assertMarketAdStatus(t, get(t, fmt.Sprintf("/api/v1/market/ads/%d", adID), ownerToken), http.StatusOK, "DRAFT")
		assertStatus(t, get(t, fmt.Sprintf("/api/v1/market/ads/%d", adID), secondOwnerToken), http.StatusNotFound)
		assertStatus(t, patchReq(t, fmt.Sprintf("/api/v1/market/ads/%d", adID), map[string]any{"title": "Illegal edit"}, secondOwnerToken), http.StatusForbidden)
		assertMarketAdStatus(t, post(t, fmt.Sprintf("/api/v1/market/ads/%d/publish", adID), nil, adminToken), http.StatusOK, "PUBLISHED")
		assertMarketAdStatus(t, post(t, fmt.Sprintf("/api/v1/market/ads/%d/pause", adID), nil, adminToken), http.StatusOK, "PAUSED")
	})

	t.Run("manager edits and publishes nursery ad", func(t *testing.T) {
		resp := patchReq(t, fmt.Sprintf("/api/v1/market/ads/%d", adID), map[string]any{"description": "Updated by manager"}, managerToken)
		assertMarketAdStatus(t, resp, http.StatusOK, "PAUSED")
		assertMarketAdStatus(t, post(t, fmt.Sprintf("/api/v1/market/ads/%d/publish", adID), nil, managerToken), http.StatusOK, "PUBLISHED")
	})

	t.Run("published ad social rules", func(t *testing.T) {
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/market/ads/%d/save", adID), nil, secondOwnerToken), http.StatusOK)
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/market/ads/%d/save", adID), nil, secondOwnerToken), http.StatusOK)
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/market/ads/%d/enquiries", adID), map[string]any{"message": "self enquiry"}, ownerToken), http.StatusBadRequest)
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/market/ads/%d/report", adID), map[string]any{"reason": "SPAM"}, ownerToken), http.StatusBadRequest)
	})

	enquiryID := createMarketEnquiry(t, secondOwnerToken, adID, "Need 25 ficus plants")

	t.Run("cross nursery enquiry access and reply", func(t *testing.T) {
		assertStatus(t, get(t, fmt.Sprintf("/api/v1/market/enquiries/%d", enquiryID), secondOwnerToken), http.StatusOK)
		assertStatus(t, get(t, fmt.Sprintf("/api/v1/market/enquiries/%d", enquiryID), ownerToken), http.StatusOK)
		assertStatus(t, get(t, fmt.Sprintf("/api/v1/market/enquiries/%d", enquiryID), managerToken), http.StatusOK)
		assertStatus(t, get(t, fmt.Sprintf("/api/v1/market/enquiries/%d", enquiryID), adminToken), http.StatusOK)
		assertStatus(t, get(t, fmt.Sprintf("/api/v1/market/enquiries/%d", enquiryID), driverToken), http.StatusForbidden)

		reply := post(t, fmt.Sprintf("/api/v1/market/enquiries/%d/reply", enquiryID), map[string]any{"body": "Available for dispatch tomorrow"}, managerToken)
		assertStatus(t, reply, http.StatusCreated)
		assertEnquiryStatus(t, get(t, fmt.Sprintf("/api/v1/market/enquiries/%d", enquiryID), ownerToken), http.StatusOK, "IN_PROGRESS")

		assertStatus(t, post(t, fmt.Sprintf("/api/v1/market/enquiries/%d/cancel", enquiryID), nil, ownerToken), http.StatusForbidden)
		assertEnquiryStatus(t, post(t, fmt.Sprintf("/api/v1/market/enquiries/%d/close", enquiryID), nil, managerToken), http.StatusOK, "CLOSED")
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/market/enquiries/%d/reply", enquiryID), map[string]any{"body": "too late"}, ownerToken), http.StatusBadRequest)
	})

	t.Run("ad pause resume archive lifecycle", func(t *testing.T) {
		assertMarketAdStatus(t, post(t, fmt.Sprintf("/api/v1/market/ads/%d/pause", adID), nil, ownerToken), http.StatusOK, "PAUSED")
		assertMarketAdStatus(t, post(t, fmt.Sprintf("/api/v1/market/ads/%d/resume", adID), nil, managerToken), http.StatusOK, "PUBLISHED")
		assertMarketAdStatus(t, post(t, fmt.Sprintf("/api/v1/market/ads/%d/archive", adID), nil, ownerToken), http.StatusOK, "ARCHIVED")
		assertStatus(t, patchReq(t, fmt.Sprintf("/api/v1/market/ads/%d", adID), map[string]any{"title": "Archived edit"}, ownerToken), http.StatusBadRequest)
	})

	cancelAdID := createMarketAd(t, ownerToken, "Cancellation palms "+time.Now().Format("150405.000"))
	assertMarketAdStatus(t, post(t, fmt.Sprintf("/api/v1/market/ads/%d/publish", cancelAdID), nil, ownerToken), http.StatusOK, "PUBLISHED")
	cancelEnquiryID := createMarketEnquiry(t, secondOwnerToken, cancelAdID, "Please reserve 10 palms")

	t.Run("enquiring nursery cancels its enquiry", func(t *testing.T) {
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/market/enquiries/%d/cancel", cancelEnquiryID), nil, ownerToken), http.StatusForbidden)
		assertEnquiryStatus(t, post(t, fmt.Sprintf("/api/v1/market/enquiries/%d/cancel", cancelEnquiryID), nil, secondOwnerToken), http.StatusOK, "CANCELLED")
		assertStatus(t, post(t, fmt.Sprintf("/api/v1/market/enquiries/%d/reply", cancelEnquiryID), map[string]any{"body": "too late"}, secondOwnerToken), http.StatusBadRequest)
	})

	assertMarketAdStatus(t, post(t, fmt.Sprintf("/api/v1/market/ads/%d/archive", cancelAdID), nil, ownerToken), http.StatusOK, "ARCHIVED")
}

func createMarketAd(t *testing.T, token, title string) int64 {
	t.Helper()
	resp := post(t, "/api/v1/market/ads", map[string]any{
		"plant_name":     "Ficus",
		"title":          title,
		"description":    "Lifecycle integration fixture",
		"quantity":       50,
		"price_per_unit": 125,
		"price_unit":     "PER_PLANT",
		"photos":         []string{},
	}, token)
	assertStatus(t, resp, http.StatusCreated)
	var result marketAdEnvelope
	decode(t, resp, &result)
	if result.Ad.ID == 0 {
		t.Fatal("create market ad returned id 0")
	}
	if result.Ad.Status != "DRAFT" {
		t.Fatalf("new market ad status: got %q, want DRAFT", result.Ad.Status)
	}
	return result.Ad.ID
}

func createMarketEnquiry(t *testing.T, token string, adID int64, message string) int64 {
	t.Helper()
	resp := post(t, fmt.Sprintf("/api/v1/market/ads/%d/enquiries", adID), map[string]any{
		"message":         message,
		"quantity_needed": 25,
	}, token)
	assertStatus(t, resp, http.StatusCreated)
	var result marketEnquiryEnvelope
	decode(t, resp, &result)
	if result.Enquiry.ID == 0 {
		t.Fatal("create market enquiry returned id 0")
	}
	if result.Enquiry.Status != "NEW" {
		t.Fatalf("new enquiry status: got %q, want NEW", result.Enquiry.Status)
	}
	return result.Enquiry.ID
}

func assertMarketAdStatus(t *testing.T, resp *http.Response, wantHTTP int, wantStatus string) {
	t.Helper()
	assertStatus(t, resp, wantHTTP)
	if wantHTTP < 200 || wantHTTP >= 300 {
		return
	}
	var result marketAdEnvelope
	decode(t, resp, &result)
	if result.Ad.Status != wantStatus {
		t.Fatalf("market ad status: got %q, want %q", result.Ad.Status, wantStatus)
	}
}

func assertEnquiryStatus(t *testing.T, resp *http.Response, wantHTTP int, wantStatus string) {
	t.Helper()
	assertStatus(t, resp, wantHTTP)
	if wantHTTP < 200 || wantHTTP >= 300 {
		return
	}
	var result marketEnquiryEnvelope
	decode(t, resp, &result)
	if result.Enquiry.Status != wantStatus {
		t.Fatalf("market enquiry status: got %q, want %q", result.Enquiry.Status, wantStatus)
	}
}
