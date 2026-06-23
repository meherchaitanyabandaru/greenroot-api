package publiccode

import (
	"testing"
	"time"
)

func TestFormatSequential(t *testing.T) {
	got := Format(Users, 7, time.Time{})
	if got != "USR-000007" {
		t.Fatalf("expected USR-000007, got %s", got)
	}
}

func TestFormatDateBased(t *testing.T) {
	at := time.Date(2026, 6, 22, 10, 30, 0, 0, time.UTC)
	got := Format(Orders, 12, at)
	if got != "ORD-20260622-0012" {
		t.Fatalf("expected ORD-20260622-0012, got %s", got)
	}
}

func TestAllSpecsHaveValidShape(t *testing.T) {
	specs := []Spec{
		Users, Plants, Nurseries, Inventory, Requests, Orders, Dispatches,
		Payments, Drivers, Vehicles, Attachments, Notifications, UserSubscriptions,
	}

	for _, spec := range specs {
		if spec.Key == "" || spec.Prefix == "" || spec.Width <= 0 {
			t.Fatalf("invalid spec: %+v", spec)
		}
	}
}
