// Package messaging is the channel-agnostic core for outbound user messages.
//
// Business modules raise an Event ("PAYMENT_FAILED" with semantic values like
// amount and payment_code). The Notifier expands those values once
// (ExpandValues), resolves the user's reachable addresses, and fans the event
// out to every Channel (SMS, email, ...) that has an active template with the
// same code. Channel rules — TRAI DLT for SMS, HTML escaping for email — stay
// inside each channel.
package messaging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"
)

// ErrSkipped means the channel cannot or need not deliver this message (no
// address for the user, or no active template for the event). It is not a
// failure.
var ErrSkipped = errors.New("message skipped")

// Recipient carries only addresses the user can actually be reached at:
// Email is empty unless the address is verified.
type Recipient struct {
	UserID int64
	Mobile string
	Email  string
	// SignedIn: has used the app (push reaches them; NotOnAppOnly events skip them).
	SignedIn bool
}

// Message is one event addressed to one recipient on one channel.
type Message struct {
	TemplateCode  string
	Recipient     Recipient
	Values        map[string]string
	Sensitive     []string // value names masked in logs (e.g. "otp")
	ReferenceType string
	ReferenceID   *int64
}

type Result struct {
	MessageID int64
	Status    string
}

type Channel interface {
	Name() string
	Deliver(ctx context.Context, msg Message) (Result, error)
}

// RecipientLookup resolves a user's reachable addresses.
type RecipientLookup interface {
	Recipient(ctx context.Context, userID int64) (Recipient, error)
}

// Event is what business modules raise; Values hold semantic data, see ExpandValues.
type Event struct {
	Code   string
	UserID int64
	// Mobile reaches someone with no account yet (UserID 0), e.g. a quotation
	// sent to a number the nursery typed in.
	Mobile string
	// NotOnAppOnly: for customers who have never signed in; app users get the
	// same news by push, so they are not sent an SMS too.
	NotOnAppOnly  bool
	Values        map[string]string
	Sensitive     []string
	ReferenceType string
	ReferenceID   *int64
}

// Outcome is one channel's result for an event.
type Outcome struct {
	Channel string
	Result  Result
	Err     error // nil, ErrSkipped, or a real failure
}

type Notifier struct {
	channels   []Channel
	recipients RecipientLookup
}

func NewNotifier(recipients RecipientLookup, channels ...Channel) *Notifier {
	return &Notifier{channels: channels, recipients: recipients}
}

// Notify delivers the event on every channel. A failing channel never stops
// the others.
func (n *Notifier) Notify(ctx context.Context, ev Event) ([]Outcome, error) {
	recipient := Recipient{Mobile: ev.Mobile}
	if ev.UserID != 0 {
		var err error
		if recipient, err = n.recipients.Recipient(ctx, ev.UserID); err != nil {
			return nil, fmt.Errorf("resolve recipient %d: %w", ev.UserID, err)
		}
	}
	if ev.NotOnAppOnly && recipient.SignedIn {
		return nil, nil
	}
	msg := Message{
		TemplateCode:  ev.Code,
		Recipient:     recipient,
		Values:        ExpandValues(ev.Values),
		Sensitive:     ev.Sensitive,
		ReferenceType: ev.ReferenceType,
		ReferenceID:   ev.ReferenceID,
	}
	outcomes := make([]Outcome, 0, len(n.channels))
	for _, ch := range n.channels {
		res, err := ch.Deliver(ctx, msg)
		if err != nil && !errors.Is(err, ErrSkipped) {
			slog.Warn("message not delivered", "channel", ch.Name(), "event", ev.Code, "user_id", ev.UserID, "error", err)
		}
		outcomes = append(outcomes, Outcome{Channel: ch.Name(), Result: res, Err: err})
	}
	return outcomes, nil
}

// NotifyAsync runs Notify detached from the request, so gateway latency or
// failure never affects the business operation that raised the event.
func (n *Notifier) NotifyAsync(ev Event) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := n.Notify(ctx, ev); err != nil {
			slog.Warn("notify failed", "event", ev.Code, "user_id", ev.UserID, "error", err)
		}
	}()
}

// ExpandValues adds channel-friendly derived values next to the originals, so
// one event serves every channel:
//
//	amount "1250.5"                → amount_rupees "1250", amount_paise "50"
//	<x>_code "ORD-20261016-0008"   → <x>_date "20261016", <x>_seq "0008"
//	mobile "9876543210"            → mobile_last4 "3210"
//
// SMS needs the split forms because DLT {#numeric#} admits digits only; email
// templates usually use the originals.
func ExpandValues(values map[string]string) map[string]string {
	out := make(map[string]string, len(values)*2)
	for k, v := range values {
		out[k] = v
	}
	for k, v := range values {
		switch {
		case k == "amount":
			if amount, err := strconv.ParseFloat(v, 64); err == nil {
				out["amount_rupees"], out["amount_paise"] = AmountParts(amount)
			}
		case k == "mobile" && len(v) >= 4:
			out["mobile_last4"] = v[len(v)-4:]
		case strings.HasSuffix(k, "_code"):
			if parts := CodeParts(v); len(parts) == 2 {
				base := strings.TrimSuffix(k, "_code")
				out[base+"_date"], out[base+"_seq"] = parts[0], parts[1]
			}
		}
	}
	return out
}

// AmountParts splits a rupee amount into whole rupees and two-digit paise.
func AmountParts(amount float64) (rupees string, paise string) {
	total := int64(math.Round(amount * 100))
	return fmt.Sprintf("%d", total/100), fmt.Sprintf("%02d", total%100)
}

// FormatAmount renders an amount for event values ("1250.50").
func FormatAmount(amount float64) string {
	r, p := AmountParts(amount)
	return r + "." + p
}

// CodeParts splits a public code like "ORD-20260725-0008" into the parts after
// the prefix ("20260725", "0008").
func CodeParts(code string) []string {
	parts := strings.Split(code, "-")
	if len(parts) <= 1 {
		return nil
	}
	return parts[1:]
}
