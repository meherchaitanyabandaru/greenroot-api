package sms

import (
	"strings"
	"testing"
)

const loginOTPBody = "{#numeric#} is your OTP to log in to GreenRoot. It is valid for {#numeric#} minutes. Do not share this OTP with anyone. - GreenRoot"

func TestParseTags(t *testing.T) {
	tags, err := ParseTags("Hi {#alphanumeric#}, OTP {#numeric#}, see {#url#}")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tags, ",") != "alphanumeric,numeric,url" {
		t.Fatalf("tags = %v", tags)
	}
	if _, err := ParseTags("OTP {#var#}"); err == nil || !strings.Contains(err.Error(), "untyped") {
		t.Fatalf("legacy {#var#} must be rejected, got %v", err)
	}
	if _, err := ParseTags("OTP {#number#}"); err == nil {
		t.Fatal("unknown tag must be rejected")
	}
}

func TestValidateValue(t *testing.T) {
	cases := []struct {
		tag, value string
		ok         bool
	}{
		{TagNumeric, "123456", true},
		{TagNumeric, "12.50", false},
		{TagNumeric, "", false},
		{TagNumeric, strings.Repeat("1", 31), false},
		{TagAlphanumeric, "ORD123", true},
		{TagAlphanumeric, "ORD-123", false},
		{TagAlphanumeric, "Ravi Kumar", false},
		{TagEmail, "help@greenroot.in", true},
		{TagEmail, "not-an-email", false},
		{TagURL, "https://greenroot.in/o/1", true},
		{TagURL, "https://greenroot.in.evil.com", false},
		{TagCBN, "18001234567", true},
		{TagCBN, "18009999999", false},
	}
	for _, c := range cases {
		err := ValidateValue(c.tag, c.value, []string{"https://greenroot.in"}, []string{"18001234567"})
		if (err == nil) != c.ok {
			t.Errorf("ValidateValue(%s, %q) err=%v, want ok=%v", c.tag, c.value, err, c.ok)
		}
	}
}

func TestRender_MasksSensitiveAndCountsParts(t *testing.T) {
	spec := TemplateSpec{Body: loginOTPBody, Variables: []string{"otp", "valid_minutes"}, Category: CategoryServiceImplicit}
	r, err := Render(spec, map[string]string{"otp": "482913", "valid_minutes": "5"}, map[string]bool{"otp": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.Body, "482913 is your OTP") || !strings.Contains(r.Body, "valid for 5 minutes") {
		t.Fatalf("body = %q", r.Body)
	}
	if strings.Contains(r.Masked, "482913") || !strings.HasPrefix(r.Masked, "****** is your OTP") {
		t.Fatalf("masked = %q", r.Masked)
	}
	if r.Parts != 1 {
		t.Fatalf("login OTP must fit one SMS, got %d parts (%d chars)", r.Parts, r.Chars)
	}
}

func TestRender_RejectsBadValue(t *testing.T) {
	spec := TemplateSpec{Body: loginOTPBody, Variables: []string{"otp", "valid_minutes"}}
	if _, err := Render(spec, map[string]string{"otp": "12a456", "valid_minutes": "5"}, nil); err == nil {
		t.Fatal("non-digit OTP must be rejected")
	}
	if _, err := Render(spec, map[string]string{"otp": "123456"}, nil); err == nil {
		t.Fatal("missing variable must be rejected")
	}
}

func TestSegments(t *testing.T) {
	if _, p := Segments(strings.Repeat("a", 160), false); p != 1 {
		t.Errorf("160 GSM chars = %d parts, want 1", p)
	}
	if _, p := Segments(strings.Repeat("a", 161), false); p != 2 {
		t.Errorf("161 GSM chars = %d parts, want 2", p)
	}
	if c, _ := Segments("[]", false); c != 4 {
		t.Errorf("extended GSM chars count double, got %d", c)
	}
	if _, p := Segments(strings.Repeat("न", 70), true); p != 1 {
		t.Errorf("70 UCS-2 chars = %d parts, want 1", p)
	}
	if _, p := Segments(strings.Repeat("न", 71), true); p != 2 {
		t.Errorf("71 UCS-2 chars = %d parts, want 2", p)
	}
}

func TestValidateSpec(t *testing.T) {
	ok := TemplateSpec{Body: loginOTPBody, Variables: []string{"otp", "valid_minutes"}, Category: CategoryServiceImplicit, HeaderType: HeaderService}
	if errs, _ := ValidateSpec(ok); len(errs) != 0 {
		t.Fatalf("valid spec has errors: %v", errs)
	}

	bad := []struct {
		name string
		spec TemplateSpec
		want string
	}{
		{"count mismatch", TemplateSpec{Body: loginOTPBody, Variables: []string{"otp"}}, "variable names"},
		{"duplicate name", TemplateSpec{Body: loginOTPBody, Variables: []string{"otp", "otp"}}, "used twice"},
		{"bad name", TemplateSpec{Body: "Code {#numeric#}", Variables: []string{"OTP"}}, "lower_snake_case"},
		{"unicode without flag", TemplateSpec{Body: "आपका OTP {#numeric#}", Variables: []string{"otp"}}, "GSM alphabet"},
		{"url without whitelist", TemplateSpec{Body: "Track {#url#}", Variables: []string{"link"}}, "no URL"},
		{"header mismatch", TemplateSpec{Body: "Code {#numeric#}", Variables: []string{"otp"}, Category: CategoryPromotional, HeaderType: HeaderService}, "cannot send"},
	}
	for _, c := range bad {
		errs, _ := ValidateSpec(c.spec)
		if !strings.Contains(strings.Join(errs, "; "), c.want) {
			t.Errorf("%s: errors %v, want one containing %q", c.name, errs, c.want)
		}
	}

	unicode := TemplateSpec{Body: "आपका OTP {#numeric#}", Variables: []string{"otp"}, IsUnicode: true}
	if errs, _ := ValidateSpec(unicode); len(errs) != 0 {
		t.Fatalf("unicode template flagged: %v", errs)
	}
	if _, warnings := ValidateSpec(TemplateSpec{Body: "Big sale today", Category: CategoryPromotional}); len(warnings) == 0 {
		t.Fatal("promotional template without opt-out wording should warn")
	}
}

// Every seeded template must render as a single SMS with realistic values,
// so OTP and alerts never become multi-part (more cost, slower delivery).
func TestSeedTemplatesFitOneSMS(t *testing.T) {
	seeds := []TemplateSpec{
		{Body: loginOTPBody, Variables: []string{"otp", "valid_minutes"}},
		{Body: "{#numeric#} is your OTP to create your GreenRoot account. It is valid for {#numeric#} minutes. Do not share this OTP with anyone. - GreenRoot", Variables: []string{"otp", "valid_minutes"}},
		{Body: "Welcome to GreenRoot. Your account for the mobile number ending {#numeric#} has been created successfully. - GreenRoot", Variables: []string{"mobile_last4"}},
		{Body: "Payment of Rs.{#numeric#}.{#numeric#} received for GreenRoot order ORD-{#numeric#}-{#numeric#}. Payment ref PAY-{#numeric#}-{#numeric#}. - GreenRoot", Variables: []string{"amount_rupees", "amount_paise", "order_date", "order_seq", "payment_date", "payment_seq"}},
		{Body: "Payment of Rs.{#numeric#}.{#numeric#} received for your GreenRoot subscription. Payment ref PAY-{#numeric#}-{#numeric#}. - GreenRoot", Variables: []string{"amount_rupees", "amount_paise", "payment_date", "payment_seq"}},
		{Body: "Your payment of Rs.{#numeric#}.{#numeric#} on GreenRoot (ref PAY-{#numeric#}-{#numeric#}) was not successful. Please try again. - GreenRoot", Variables: []string{"amount_rupees", "amount_paise", "payment_date", "payment_seq"}},
	}
	values := map[string]string{
		"otp": "123456", "valid_minutes": "5", "mobile_last4": "0000",
		"amount_rupees": "1250000", "amount_paise": "50",
		"order_date": "20261016", "order_seq": "12345", "payment_date": "20261016", "payment_seq": "12345",
	}
	for _, spec := range seeds {
		spec.Category = CategoryServiceImplicit
		if errs, _ := ValidateSpec(spec); len(errs) != 0 {
			t.Errorf("seed %q invalid: %v", spec.Body[:30], errs)
			continue
		}
		r, err := Render(spec, values, nil)
		if err != nil {
			t.Errorf("seed %q render: %v", spec.Body[:30], err)
			continue
		}
		if r.Parts != 1 {
			t.Errorf("seed %q is %d chars / %d parts", spec.Body[:30], r.Chars, r.Parts)
		}
	}
}

func TestHelpers(t *testing.T) {
	for in, want := range map[string]string{"+91 98765 43210": "9876543210", "919876543210": "9876543210", "09876543210": "9876543210", "9876543210": "9876543210"} {
		if got, ok := NormalizeMobile(in); !ok || got != want {
			t.Errorf("NormalizeMobile(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := NormalizeMobile("1234567890"); ok {
		t.Error("numbers starting 1-5 are not Indian mobiles")
	}
	if r, p := AmountParts(1250.5); r != "1250" || p != "50" {
		t.Errorf("AmountParts(1250.5) = %s.%s", r, p)
	}
	if r, p := AmountParts(0.07); r != "0" || p != "07" {
		t.Errorf("AmountParts(0.07) = %s.%s", r, p)
	}
	if r, p := AmountParts(19.999); r != "20" || p != "00" {
		t.Errorf("AmountParts(19.999) = %s.%s", r, p)
	}
	if parts := CodeParts("ORD-20260725-0008"); len(parts) != 2 || parts[0] != "20260725" || parts[1] != "0008" {
		t.Errorf("CodeParts = %v", parts)
	}
}
