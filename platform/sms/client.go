// Package sms is the operator-gateway boundary for outbound SMS. Everything
// TRAI DLT-specific (templates, consent, rendering) lives in
// internal/modules/sms; a Provider only delivers an already-rendered message.
package sms

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Message is one rendered SMS carrying the DLT identifiers every Indian
// gateway requires on submission.
type Message struct {
	To             string // 10-digit Indian mobile number, no country code
	Body           string // must match the DLT template with variables filled in
	Header         string // DLT sender ID, e.g. GRROOT
	PEID           string // DLT Principal Entity ID
	TemplateID     string // DLT content template ID
	TelemarketerID string // optional; for PE-TM chain binding
	Unicode        bool
}

type Result struct {
	ProviderMessageID string
}

type Provider interface {
	Name() string
	// Live reports whether messages actually reach handsets. Callers relax
	// DLT-registration checks and use a fixed dev OTP only when this is false.
	Live() bool
	Send(ctx context.Context, msg Message) (Result, error)
}

const ProviderMock = "mock"

// New returns the provider named by SMS_PROVIDER. Unknown names fail fast so a
// typo can never silently fall back to the mock in production.
func New(name string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", ProviderMock:
		return MockProvider{}, nil
	case ProviderAWS:
		return newAWSProvider()
	}
	return nil, fmt.Errorf("unsupported SMS_PROVIDER %q (supported: %s, %s)", name, ProviderMock, ProviderAWS)
}

// MockProvider never contacts a network. It logs identifiers only, never the
// body, so OTPs do not end up in log files.
type MockProvider struct{}

func (MockProvider) Name() string { return ProviderMock }
func (MockProvider) Live() bool   { return false }

func (MockProvider) Send(_ context.Context, msg Message) (Result, error) {
	id := fmt.Sprintf("mock-%d", time.Now().UnixNano())
	slog.Info("mock sms", "to", maskMobile(msg.To), "header", msg.Header, "dlt_template_id", msg.TemplateID, "provider_message_id", id)
	return Result{ProviderMessageID: id}, nil
}

func maskMobile(mobile string) string {
	if len(mobile) < 4 {
		return "****"
	}
	return strings.Repeat("*", len(mobile)-4) + mobile[len(mobile)-4:]
}
