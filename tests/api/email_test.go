package apitest

import (
	"net/http"
	"strings"
	"testing"
)

type emailTemplate struct {
	ID           int64    `json:"id"`
	TemplateCode string   `json:"template_code"`
	TemplateName string   `json:"template_name"`
	Subject      string   `json:"subject"`
	BodyHTML     string   `json:"body_html"`
	BodyText     string   `json:"body_text"`
	Variables    []string `json:"variables"`
	IsActive     bool     `json:"is_active"`
}

func TestEmail_AdminOnly(t *testing.T) {
	assertStatus(t, get(t, "/api/v1/email/status", login(t, buyerPhone)), http.StatusForbidden)
	assertStatus(t, get(t, "/api/v1/email/templates", ""), http.StatusUnauthorized)
}

func TestEmail_TemplateUpdateRoundTrip(t *testing.T) {
	token := login(t, adminPhone)
	var list struct {
		Templates []emailTemplate `json:"templates"`
	}
	decode(t, get(t, "/api/v1/email/templates", token), &list)
	var tpl emailTemplate
	for _, item := range list.Templates {
		if item.TemplateCode == "PAYMENT_FAILED" {
			tpl = item
		}
	}
	if tpl.ID == 0 {
		t.Fatal("PAYMENT_FAILED email template not seeded")
	}
	if strings.Join(tpl.Variables, ",") != "amount,payment_code" {
		t.Fatalf("variables = %v", tpl.Variables)
	}
	path := url("/api/v1/email/templates/%d", tpl.ID)
	original := tpl
	t.Cleanup(func() { putReq(t, path, original, token).Body.Close() })

	tpl.Subject = "Payment {{payment_code}} failed"
	resp := putReq(t, path, tpl, token)
	assertStatus(t, resp, http.StatusOK)
	var updated struct {
		Template emailTemplate `json:"template"`
	}
	decode(t, resp, &updated)
	if updated.Template.Subject != tpl.Subject {
		t.Fatalf("subject not saved: %q", updated.Template.Subject)
	}

	tpl.BodyHTML = "<script>alert(1)</script>"
	assertStatus(t, putReq(t, path, tpl, token), http.StatusBadRequest)
}

// Verifies an address through the real SQL path, then clears it again.
func TestEmail_VerifyFlow(t *testing.T) {
	token := login(t, driverPhone)
	t.Cleanup(func() {
		putReq(t, "/api/v1/users/me", map[string]any{"first_name": currentFirstName(t, token), "email": ""}, token).Body.Close()
	})

	resp := post(t, "/api/v1/users/me/email/send-code", map[string]any{"email": "Driver.Test@Example.in"}, token)
	assertStatus(t, resp, http.StatusOK)
	var sent struct {
		MockCode    string `json:"mock_code"`
		DeliveredTo string `json:"delivered_to"`
	}
	decode(t, resp, &sent)
	if sent.DeliveredTo != "driver.test@example.in" || sent.MockCode == "" {
		t.Fatalf("send-code = %+v", sent)
	}

	assertStatus(t, post(t, "/api/v1/users/me/email/verify", map[string]any{"email": sent.DeliveredTo, "code": "000000"}, token), http.StatusBadRequest)

	resp = post(t, "/api/v1/users/me/email/verify", map[string]any{"email": sent.DeliveredTo, "code": sent.MockCode}, token)
	assertStatus(t, resp, http.StatusOK)
	var verified struct {
		User struct {
			Email         string `json:"email"`
			EmailVerified bool   `json:"email_verified"`
		} `json:"user"`
	}
	decode(t, resp, &verified)
	if !verified.User.EmailVerified || verified.User.Email != "driver.test@example.in" {
		t.Fatalf("user = %+v", verified.User)
	}

	// Another account cannot claim the same address.
	assertStatus(t, post(t, "/api/v1/users/me/email/send-code", map[string]any{"email": "driver.test@example.in"}, login(t, buyerPhone)), http.StatusConflict)
}

func currentFirstName(t *testing.T, token string) string {
	t.Helper()
	var me struct {
		User struct {
			FirstName string `json:"first_name"`
		} `json:"user"`
	}
	decode(t, get(t, "/api/v1/users/me", token), &me)
	return me.User.FirstName
}
