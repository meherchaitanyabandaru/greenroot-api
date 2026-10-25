package notifications

import (
	"context"
	"errors"
	"testing"

	"github.com/meherchaitanyabandaru/greenroot-api/platform/fcm"
)

type fakePusher struct {
	results map[string]error
	sent    []string
}

func (f *fakePusher) Send(_ context.Context, m fcm.Message) error {
	f.sent = append(f.sent, m.Token)
	return f.results[m.Token]
}

type fakePushStore struct {
	devices  []Device
	retired  []string
	delivery string
}

func (f *fakePushStore) ListDevices(context.Context, int64) ([]Device, error) { return f.devices, nil }
func (f *fakePushStore) DeactivateDeviceToken(_ context.Context, token string) error {
	f.retired = append(f.retired, token)
	return nil
}
func (f *fakePushStore) MarkDelivery(_ context.Context, _ int64, status string) error {
	f.delivery = status
	return nil
}

func pushNote() Notification {
	uid, title, msg := int64(9), "Delivery marked without your confirmation", "DSP-1: customer not reachable"
	return Notification{ID: 1, UserID: &uid, Title: &title, Message: &msg, Type: "ORDER_DELIVERED"}
}

func TestPushSenderSendsToEveryDeviceAndRetiresDeadTokens(t *testing.T) {
	push := &fakePusher{results: map[string]error{"dead": fcm.ErrUnregistered}}
	store := &fakePushStore{devices: []Device{{ID: 1, FCMToken: "phone"}, {ID: 2, FCMToken: "dead"}, {ID: 3, FCMToken: "tablet"}}}
	if err := NewPushSender(push, store, nil).Send(context.Background(), pushNote()); err != nil {
		t.Fatal(err)
	}
	if len(push.sent) != 3 || store.delivery != DeliverySent {
		t.Fatalf("sent to %v, delivery %q", push.sent, store.delivery)
	}
	if len(store.retired) != 1 || store.retired[0] != "dead" {
		t.Fatalf("retired %v, want [dead]", store.retired)
	}
}

func TestPushSenderOutcomes(t *testing.T) {
	ctx := context.Background()

	store := &fakePushStore{}
	if err := NewPushSender(&fakePusher{}, store, nil).Send(ctx, pushNote()); err != nil || store.delivery != DeliveryNoDevice {
		t.Fatalf("no device: %v %q", err, store.delivery)
	}

	store = &fakePushStore{devices: []Device{{FCMToken: "phone"}}}
	err := NewPushSender(&fakePusher{results: map[string]error{"phone": errors.New("HTTP 503")}}, store, nil).Send(ctx, pushNote())
	if err == nil || store.delivery != DeliveryFailed {
		t.Fatalf("outage must be reported for a retry: %v %q", err, store.delivery)
	}

	store = &fakePushStore{devices: []Device{{FCMToken: "phone"}}}
	n := pushNote()
	n.UserID = nil
	if err := NewPushSender(&fakePusher{}, store, nil).Send(ctx, n); err != nil || store.delivery != "" {
		t.Fatalf("no recipient: inbox only, got %v %q", err, store.delivery)
	}
}
