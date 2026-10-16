package apitest

import (
	"net/http"
	"strings"
	"testing"
)

type smsTemplate struct {
	ID                 int64    `json:"id"`
	TemplateCode       string   `json:"template_code"`
	TemplateName       string   `json:"template_name"`
	Category           string   `json:"category"`
	HeaderID           *int64   `json:"header_id"`
	Body               string   `json:"body"`
	Variables          []string `json:"variables"`
	DLTTemplateID      *string  `json:"dlt_template_id"`
	RegistrationStatus string   `json:"registration_status"`
	ApprovedAt         *string  `json:"approved_at"`
}

func TestSMS_AdminOnly(t *testing.T) {
	resp := get(t, "/api/v1/sms/status", login(t, buyerPhone))
	assertStatus(t, resp, http.StatusForbidden)
	resp = get(t, "/api/v1/sms/templates", "")
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestSMS_SeededSystemTemplates(t *testing.T) {
	resp := get(t, "/api/v1/sms/templates", login(t, adminPhone))
	assertStatus(t, resp, http.StatusOK)
	var result struct {
		Templates []smsTemplate `json:"templates"`
	}
	decode(t, resp, &result)
	codes := map[string]bool{}
	for _, tpl := range result.Templates {
		codes[tpl.TemplateCode] = true
	}
	for _, code := range []string{"AUTH_LOGIN_OTP", "AUTH_REGISTER_OTP", "ACCOUNT_WELCOME", "PAYMENT_RECEIVED_ORDER", "PAYMENT_RECEIVED_SUBSCRIPTION", "PAYMENT_FAILED"} {
		if !codes[code] {
			t.Errorf("system template %s missing from seed", code)
		}
	}
}

// Approve → locked edit → back to DRAFT round-trips through the real SQL
// (parameter typing in UPDATE broke once; unit tests use a fake repo).
func TestSMS_TemplateApprovalLock(t *testing.T) {
	token := login(t, adminPhone)
	var list struct {
		Templates []smsTemplate `json:"templates"`
	}
	decode(t, get(t, "/api/v1/sms/templates", token), &list)
	var tpl smsTemplate
	for _, item := range list.Templates {
		if item.TemplateCode == "AUTH_LOGIN_OTP" {
			tpl = item
		}
	}
	if tpl.ID == 0 {
		t.Fatal("AUTH_LOGIN_OTP not seeded")
	}
	path := url("/api/v1/sms/templates/%d", tpl.ID)
	original := tpl
	t.Cleanup(func() { putReq(t, path, original, token).Body.Close() })

	dltID := "1207171234567890123"
	tpl.RegistrationStatus, tpl.DLTTemplateID = "APPROVED", &dltID
	resp := putReq(t, path, tpl, token)
	assertStatus(t, resp, http.StatusOK)
	var approved struct {
		Template smsTemplate `json:"template"`
	}
	decode(t, resp, &approved)
	if approved.Template.ApprovedAt == nil {
		t.Error("approved_at not stamped")
	}

	tpl.Body = strings.Replace(tpl.Body, "Do not share", "Never share", 1)
	assertStatus(t, putReq(t, path, tpl, token), http.StatusConflict)
}

func TestSMS_SendOTPLogsMaskedMessage(t *testing.T) {
	sendOTP(t, buyerPhone)
	resp := get(t, "/api/v1/sms/messages?template_code=AUTH_LOGIN_OTP&mobile="+buyerPhone+"&per_page=1", login(t, adminPhone))
	assertStatus(t, resp, http.StatusOK)
	var result struct {
		Messages []struct {
			Status      string `json:"status"`
			MessageBody string `json:"message_body"`
		} `json:"messages"`
	}
	decode(t, resp, &result)
	if len(result.Messages) == 0 {
		t.Fatal("send-otp did not log an SMS")
	}
	if m := result.Messages[0]; m.Status != "MOCKED" || !strings.HasPrefix(m.MessageBody, "******") {
		t.Fatalf("logged message = %+v", m)
	}
}

func TestSMS_PreferencesRoundTrip(t *testing.T) {
	token := login(t, buyerPhone)
	t.Cleanup(func() {
		putReq(t, "/api/v1/sms/preferences/me", map[string]any{"promotional_opt_in": false}, token).Body.Close()
	})

	resp := putReq(t, "/api/v1/sms/preferences/me", map[string]any{"promotional_opt_in": true}, token)
	assertStatus(t, resp, http.StatusOK)
	var result struct {
		Preferences struct {
			PromotionalOptIn     bool    `json:"promotional_opt_in"`
			PromotionalConsentAt *string `json:"promotional_consent_at"`
		} `json:"preferences"`
	}
	decode(t, resp, &result)
	if !result.Preferences.PromotionalOptIn || result.Preferences.PromotionalConsentAt == nil {
		t.Fatalf("opt-in not recorded: %+v", result.Preferences)
	}
}
