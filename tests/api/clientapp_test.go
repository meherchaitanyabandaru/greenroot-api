package apitest

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAppVersionPolicy(t *testing.T) {
	resp := get(t, "/api/v1/app/version-policy?platform=android&version=0.0.1", "")
	assertStatus(t, resp, http.StatusOK)
	var body struct {
		Policy struct {
			MinimumVersion string `json:"minimum_version"`
			UpdateRequired bool   `json:"update_required"`
		} `json:"policy"`
	}
	decode(t, resp, &body)
	if body.Policy.MinimumVersion == "" || !body.Policy.UpdateRequired {
		t.Fatalf("0.0.1 must be below the minimum: %+v", body.Policy)
	}
	assertStatus(t, get(t, "/api/v1/app/version-policy?platform=windows", ""), http.StatusBadRequest)
}

// TestCrashReports_EndToEnd: anonymous and signed-in reports are stored
// scrubbed, and only admins can read them.
func TestCrashReports_EndToEnd(t *testing.T) {
	marker := "it-" + time.Now().Format("150405.000000")
	report := map[string]any{
		"platform": "android", "app_version": marker, "error_type": "StateError",
		"message":     "boom for ravi@example.com otp=123456 mobile 9876543210",
		"stack_trace": "#0 main (package:greenroot/main.dart:10:5)",
		"context":     map[string]string{"screen": "order_detail"},
		"is_fatal":    true,
	}
	resp := post(t, "/api/v1/app/crash-reports", report, "")
	assertStatus(t, resp, http.StatusAccepted)
	_ = resp.Body.Close()

	buyerToken := login(t, buyerPhone)
	resp = post(t, "/api/v1/app/crash-reports", report, buyerToken)
	assertStatus(t, resp, http.StatusAccepted)
	_ = resp.Body.Close()

	bad := post(t, "/api/v1/app/crash-reports", map[string]any{"platform": "android"}, "")
	assertStatus(t, bad, http.StatusBadRequest)
	_ = bad.Body.Close()

	assertStatus(t, get(t, "/api/v1/admin/crash-reports", ""), http.StatusUnauthorized)
	assertStatus(t, get(t, "/api/v1/admin/crash-reports", buyerToken), http.StatusForbidden)

	list := get(t, "/api/v1/admin/crash-reports?app_version="+marker, login(t, adminPhone))
	assertStatus(t, list, http.StatusOK)
	var body struct {
		Items []struct {
			UserID  *int64 `json:"user_id"`
			Message string `json:"message"`
			IsFatal bool   `json:"is_fatal"`
		} `json:"items"`
		Total int `json:"total"`
	}
	decode(t, list, &body)
	if body.Total != 2 {
		t.Fatalf("want 2 reports for %s, got %d", marker, body.Total)
	}
	linked := 0
	for _, it := range body.Items {
		for _, leak := range []string{"ravi@example.com", "123456", "9876543210"} {
			if strings.Contains(it.Message, leak) {
				t.Errorf("stored message leaks %q: %q", leak, it.Message)
			}
		}
		if !it.IsFatal {
			t.Error("is_fatal lost")
		}
		if it.UserID != nil {
			linked++
		}
	}
	if linked != 1 {
		t.Errorf("exactly the signed-in report links a user; got %d", linked)
	}
}
