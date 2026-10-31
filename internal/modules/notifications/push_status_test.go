package notifications

import (
	"context"
	"errors"
	"testing"
)

type projectPusher struct{ fakePusher }

func (projectPusher) ProjectID() string { return "greenroot-dev" }

func TestPushHealth(t *testing.T) {
	cases := []struct {
		configured bool
		w          PushWindow
		want       string
	}{
		{false, PushWindow{Sent: 5}, PushHealthNotConfigured},
		{true, PushWindow{NoDevice: 4}, PushHealthNoTraffic}, // no phone signed in is not a failure
		{true, PushWindow{Sent: 10}, PushHealthOK},
		{true, PushWindow{Sent: 9, Failed: 1}, PushHealthOK}, // exactly 10% still OK
		{true, PushWindow{Sent: 8, Failed: 2}, PushHealthDegraded},
		{true, PushWindow{Sent: 1, Failed: 1}, PushHealthFailing},
	}
	for _, c := range cases {
		if got := pushHealth(c.configured, c.w); got != c.want {
			t.Errorf("pushHealth(%v, %+v) = %s, want %s", c.configured, c.w, got, c.want)
		}
	}
}

func TestPushStatus_AdminOnly(t *testing.T) {
	svc := NewService(newMock(), MockSender{}, nil)
	if _, err := svc.PushStatus(context.Background(), buyerActor(5)); !errors.Is(err, ErrForbidden) {
		t.Fatalf("buyer: want forbidden, got %v", err)
	}
}

func TestPushStatus_ConfigurationApartFromDelivery(t *testing.T) {
	mock, err := NewService(newMock(), MockSender{}, nil).PushStatus(context.Background(), adminActor(1))
	if err != nil || mock.Configured || mock.Health != PushHealthNotConfigured || mock.Stats.ActiveDevices != 3 {
		t.Fatalf("mock sender: %+v %v", mock, err)
	}
	live, err := NewService(newMock(), NewPushSender(&projectPusher{}, &fakePushStore{}, nil), nil).PushStatus(context.Background(), adminActor(1))
	if err != nil || !live.Configured || live.ProjectID != "greenroot-dev" || live.Health != PushHealthOK {
		t.Fatalf("live sender: %+v %v", live, err)
	}
}
