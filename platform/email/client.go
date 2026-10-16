// Package email is the gateway boundary for outbound email. Templates,
// escaping and logging live in internal/modules/email; a Provider only
// delivers an already-rendered message.
package email

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type Message struct {
	To       string
	FromName string
	From     string
	ReplyTo  string
	Subject  string
	HTML     string
	Text     string
}

type Result struct {
	ProviderMessageID string
}

type Provider interface {
	Name() string
	// Live reports whether mail actually leaves the system.
	Live() bool
	Send(ctx context.Context, msg Message) (Result, error)
}

const ProviderMock = "mock"

// New returns the provider named by EMAIL_PROVIDER; unknown names fail fast.
func New(name string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", ProviderMock:
		return MockProvider{}, nil
	}
	return nil, fmt.Errorf("unsupported EMAIL_PROVIDER %q (supported: %s)", name, ProviderMock)
}

// MockProvider never sends; it logs identifiers only, never the body.
type MockProvider struct{}

func (MockProvider) Name() string { return ProviderMock }
func (MockProvider) Live() bool   { return false }

func (MockProvider) Send(_ context.Context, msg Message) (Result, error) {
	id := fmt.Sprintf("mock-%d", time.Now().UnixNano())
	slog.Info("mock email", "to", maskEmail(msg.To), "provider_message_id", id)
	return Result{ProviderMessageID: id}, nil
}

func maskEmail(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at < 1 {
		return "***"
	}
	return addr[:1] + "***" + addr[at:]
}
