package market

import (
	"testing"
	"time"
)

func TestAreaKey(t *testing.T) {
	lat, lon, r := 17.38512, 78.48671, 25.4
	if got := areaKey(AdsQuery{}); got != "all" {
		t.Errorf("no location: %q", got)
	}
	if got := areaKey(AdsQuery{NearLat: &lat, NearLon: &lon}); got != "all" {
		t.Errorf("no radius: %q", got)
	}
	if got := areaKey(AdsQuery{NearLat: &lat, NearLon: &lon, RadiusKM: &r}); got != "17.385,78.487,25" {
		t.Errorf("rounded key: %q", got)
	}
}

func TestFirstNonNil(t *testing.T) {
	a, b := 1, 2
	if got := firstNonNil(&a, &b); *got != 1 {
		t.Errorf("request value wins: %d", *got)
	}
	if got := firstNonNil(nil, &b); *got != 2 {
		t.Errorf("falls back to stored: %d", *got)
	}
}

func TestInt64Value(t *testing.T) {
	for in, want := range map[any]int64{nil: 0, "42": 42, int64(7): 7, "x": 0} {
		if got := int64Value(in); got != want {
			t.Errorf("int64Value(%v) = %d, want %d", in, got, want)
		}
	}
}

func TestDaysRemaining(t *testing.T) {
	if daysRemaining(nil) != nil {
		t.Error("no expiry → nil")
	}
	soon := time.Now().Add(2 * time.Hour)
	if got := daysRemaining(&soon); *got != 1 {
		t.Errorf("under a day left counts as 1, got %d", *got)
	}
	later := time.Now().Add(10*24*time.Hour + time.Hour)
	if got := daysRemaining(&later); *got != 10 {
		t.Errorf("10 days: got %d", *got)
	}
	gone := time.Now().Add(-2 * time.Hour)
	if got := daysRemaining(&gone); *got != 0 {
		t.Errorf("expired: got %d", *got)
	}
}

func TestCanDeleteAd(t *testing.T) {
	cases := []struct {
		ad   Ad
		want bool
	}{
		{Ad{Status: StatusDraft, EnquiryCount: 3}, true},
		{Ad{Status: StatusPublished}, true},
		{Ad{Status: StatusPublished, EnquiryCount: 1}, false},
		{Ad{Status: StatusPaused}, true},
		{Ad{Status: StatusArchived}, false},
	}
	for _, c := range cases {
		if got := canDeleteAd(c.ad); got != c.want {
			t.Errorf("%s with %d enquiries: got %v", c.ad.Status, c.ad.EnquiryCount, got)
		}
	}
}

func TestDealText(t *testing.T) {
	qty, unit := 50, "PER_PLANT"
	if got := dealText(120, &qty, &unit); got != "₹120 per plant × 50" {
		t.Errorf("got %q", got)
	}
	if got := dealText(99.5, nil, nil); got != "₹99.5 each" {
		t.Errorf("got %q", got)
	}
}
