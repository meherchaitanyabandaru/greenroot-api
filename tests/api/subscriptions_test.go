package apitest

import (
	"net/http"
	"testing"
)

func TestSubscriptionPlans_Public(t *testing.T) {
	// Plans list is public — no auth needed
	resp := get(t, "/api/v1/subscription-plans", "")
	assertStatus(t, resp, http.StatusOK)

	var result struct {
		Plans []struct {
			PlanID   int64  `json:"plan_id"`
			PlanType string `json:"plan_type"`
		} `json:"plans"`
	}
	decode(t, resp, &result)

	if len(result.Plans) == 0 {
		t.Error("subscription plans list is empty — seed data required")
	}

	// Verify TRIAL and STANDARD plans exist
	types := map[string]bool{}
	for _, p := range result.Plans {
		types[p.PlanType] = true
	}
	if !types["TRIAL"] {
		t.Error("TRIAL plan not found in subscription plans")
	}
	if !types["STANDARD"] {
		t.Error("STANDARD plan not found in subscription plans")
	}
}

func TestSubscriptionMe_RequiresAuth(t *testing.T) {
	resp := get(t, "/api/v1/subscriptions/me", "")
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestSubscriptionMe_Owner(t *testing.T) {
	token := login(t, ownerPhone)
	resp := get(t, "/api/v1/subscriptions/me", token)
	// 200 if owner has a subscription, 404 if not yet subscribed
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		assertStatus(t, resp, http.StatusOK)
	}
}

func TestSubscriptionMe_Manager_Forbidden(t *testing.T) {
	token := login(t, managerPhone)
	resp := get(t, "/api/v1/subscriptions/me", token)
	// Managers cannot access subscription endpoints
	assertStatus(t, resp, http.StatusForbidden)
}

func TestSubscriptionCreate_Owner(t *testing.T) {
	// Get plans first
	plansResp := get(t, "/api/v1/subscription-plans", "")
	assertStatus(t, plansResp, http.StatusOK)

	var plans struct {
		Plans []struct {
			PlanID   int64  `json:"plan_id"`
			PlanType string `json:"plan_type"`
		} `json:"plans"`
	}
	decode(t, plansResp, &plans)

	var trialPlanID int64
	for _, p := range plans.Plans {
		if p.PlanType == "TRIAL" {
			trialPlanID = p.PlanID
			break
		}
	}
	if trialPlanID == 0 {
		t.Skip("TRIAL plan not found — seed data required")
	}

	token := login(t, ownerPhone)

	// Check if owner already has a subscription
	meResp := get(t, "/api/v1/subscriptions/me", token)
	if meResp.StatusCode == http.StatusOK {
		t.Skip("owner already has an active subscription")
	}

	body := map[string]any{
		"plan_id":       trialPlanID,
		"billing_cycle": "MONTHLY",
	}
	resp := post(t, "/api/v1/subscriptions", body, token)
	assertStatus(t, resp, http.StatusCreated)

	var sub struct {
		Status string `json:"status"`
	}
	decode(t, resp, &sub)

	if sub.Status != "TRIAL" && sub.Status != "ACTIVE" {
		t.Errorf("new subscription status: got %q, want TRIAL or ACTIVE", sub.Status)
	}
}

func TestSubscriptionList_RequiresAuth(t *testing.T) {
	resp := get(t, "/api/v1/subscriptions", "")
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestSubscriptionList_Admin(t *testing.T) {
	token := login(t, adminPhone)
	resp := get(t, "/api/v1/subscriptions", token)
	assertStatus(t, resp, http.StatusOK)
}
