package subscriptions

import (
	"database/sql"
	"testing"
	"time"
)

func TestTrialEndingMessage(t *testing.T) {
	end := time.Date(2026, 4, 9, 0, 0, 0, 0, time.UTC)
	from := sql.NullFloat64{Float64: 299, Valid: true}
	cases := map[int]string{
		0: "Your free GreenRoot trial ends today. Choose a plan from ₹299 a month to keep quoting, taking orders and delivering without a break.",
		1: "Your free GreenRoot trial ends tomorrow. Choose a plan from ₹299 a month to keep quoting, taking orders and delivering without a break.",
		2: "Your free GreenRoot trial ends on 9 Apr. Choose a plan from ₹299 a month to keep quoting, taking orders and delivering without a break.",
	}
	for days, want := range cases {
		if got := trialEndingMessage(days, end, from); got != want {
			t.Errorf("%d days: %q", days, got)
		}
	}
	if got := trialEndingMessage(1, end, sql.NullFloat64{}); got != "Your free GreenRoot trial ends tomorrow. Choose a plan to keep quoting, taking orders and delivering without a break." {
		t.Errorf("no paid plan: %q", got)
	}
}
