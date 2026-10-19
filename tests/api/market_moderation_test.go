package apitest

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestMarketModeration: a report filed by another nursery reaches the admin
// queue; only admins can see and decide it; a dismissed report leaves the ad
// live; a closed report cannot be decided again. REMOVE and strikes run in the
// journeys, which use fresh nurseries (a strike here would pile up on the
// shared fixture nursery across runs and block its posting).
func TestMarketModeration(t *testing.T) {
	ownerToken := login(t, ownerPhone)
	secondOwnerToken := login(t, secondOwnerPhone)
	adminToken := login(t, adminPhone)

	adID := createMarketAd(t, ownerToken, "Moderation fixture "+time.Now().Format("150405.000"))
	assertStatus(t, post(t, fmt.Sprintf("/api/v1/market/ads/%d/publish", adID), nil, ownerToken), http.StatusOK)
	assertStatus(t, post(t, fmt.Sprintf("/api/v1/market/ads/%d/report", adID), map[string]any{"reason": "SPAM", "notes": "same ad posted daily"}, secondOwnerToken), http.StatusOK)

	assertStatus(t, get(t, "/api/v1/market/reports", ownerToken), http.StatusForbidden)
	assertStatus(t, get(t, "/api/v1/market/reports", ""), http.StatusUnauthorized)

	var list struct {
		Reports []struct {
			ID                    int64  `json:"id"`
			AdID                  int64  `json:"ad_id"`
			Reason                string `json:"reason"`
			Status                string `json:"status"`
			ReportedByNurseryName string `json:"reported_by_nursery_name"`
		} `json:"reports"`
	}
	decode(t, get(t, "/api/v1/market/reports?status=PENDING&per_page=100", adminToken), &list)
	var reportID int64
	for _, r := range list.Reports {
		if r.AdID == adID {
			reportID = r.ID
			if r.Reason != "SPAM" || r.Status != "PENDING" || r.ReportedByNurseryName == "" {
				t.Fatalf("report fields: %+v", r)
			}
		}
	}
	if reportID == 0 {
		t.Fatalf("report for ad %d not in the admin queue", adID)
	}

	review := fmt.Sprintf("/api/v1/market/reports/%d/review", reportID)
	assertStatus(t, post(t, review, map[string]any{"decision": "REMOVE"}, ownerToken), http.StatusForbidden)
	assertStatus(t, post(t, review, map[string]any{"decision": "MAYBE"}, adminToken), http.StatusBadRequest)
	resp := post(t, review, map[string]any{"decision": "DISMISS"}, adminToken)
	var reviewed struct {
		Report struct {
			Status         string `json:"status"`
			AdStatus       string `json:"ad_status"`
			ReviewedByName string `json:"reviewed_by_name"`
		} `json:"report"`
	}
	decode(t, resp, &reviewed)
	if reviewed.Report.Status != "DISMISSED" || reviewed.Report.AdStatus != "PUBLISHED" || reviewed.Report.ReviewedByName == "" {
		t.Fatalf("review result: %+v", reviewed.Report)
	}
	assertStatus(t, post(t, review, map[string]any{"decision": "REMOVE"}, adminToken), http.StatusConflict)
}
