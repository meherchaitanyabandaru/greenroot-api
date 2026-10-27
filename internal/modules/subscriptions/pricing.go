package subscriptions

import (
	"math"
	"strings"
)

// TrialMonths is how long a newly approved nursery uses GreenRoot for free.
const TrialMonths = 1

// A billing cycle: how many months it buys and the discount on the plan's
// monthly price for paying that far ahead.
type cycleTerm struct {
	Code            string
	Months          int
	DiscountPercent int
}

var cycleTerms = []cycleTerm{
	{Code: "MONTHLY", Months: 1, DiscountPercent: 0},
	{Code: "QUARTERLY", Months: 3, DiscountPercent: 5},
	{Code: "SIX_MONTH", Months: 6, DiscountPercent: 10},
	{Code: "YEARLY", Months: 12, DiscountPercent: 15},
}

// canonicalCycle maps every accepted spelling to one code, so pricing, promo
// rules and the payment order all compare the same value. Empty stays
// SIX_MONTH, as before (older clients sent nothing).
func canonicalCycle(raw string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "MONTHLY":
		return "MONTHLY", true
	case "QUARTERLY":
		return "QUARTERLY", true
	case "SIX_MONTH", "SEMI_ANNUAL", "HALF_YEARLY", "":
		return "SIX_MONTH", true
	case "YEARLY", "ANNUAL":
		return "YEARLY", true
	case "TRIAL":
		return "TRIAL", true
	}
	return "", false
}

func termFor(cycle string) (cycleTerm, bool) {
	for _, t := range cycleTerms {
		if t.Code == cycle {
			return t, true
		}
	}
	return cycleTerm{}, false
}

// GSTPercent is added on top of every subscription charge (after the cycle
// discount and any promo). Plan prices are before tax.
const GSTPercent = 18

// withGST returns the GST on a taxable amount and the total to charge, both
// to the paisa (₹299 → ₹53.82 GST, ₹352.82 total).
func withGST(taxable float64) (gst, total float64) {
	paise := math.Round(taxable * 100)
	g := math.Round(paise * GSTPercent / 100)
	return g / 100, (paise + g) / 100
}

// cyclePrice is what a cycle costs before tax: monthly price × months, less
// the cycle discount, to the paisa.
func cyclePrice(monthly float64, t cycleTerm) float64 {
	paise := math.Round(monthly*100) * float64(t.Months) * float64(100-t.DiscountPercent) / 100
	return math.Round(paise) / 100
}

// CyclePrice is one billing option as the apps show it.
type CyclePrice struct {
	BillingCycle    string  `json:"billing_cycle"`
	Months          int     `json:"months"`
	DiscountPercent int     `json:"discount_percent"`
	ListAmount      float64 `json:"list_amount"` // monthly × months, before the discount
	Amount          float64 `json:"amount"`      // after the discount, before GST
	PerMonth        float64 `json:"per_month"`   // before GST
	GSTAmount       float64 `json:"gst_amount"`
	TotalAmount     float64 `json:"total_amount"` // what is charged: amount + GST
}

// withPrices fills the price list a paid plan offers, and the older
// six_month_price / yearly_price fields from the same math, so older app
// builds show exactly what checkout charges.
func withPrices(plan *SubscriptionPlan) {
	monthly := float64OrZero(plan.MonthlyPrice)
	if monthly <= 0 {
		plan.CyclePrices = nil
		return
	}
	plan.CyclePrices = make([]CyclePrice, 0, len(cycleTerms))
	for _, t := range cycleTerms {
		amount := cyclePrice(monthly, t)
		gst, total := withGST(amount)
		plan.CyclePrices = append(plan.CyclePrices, CyclePrice{
			GSTAmount:       gst,
			TotalAmount:     total,
			BillingCycle:    t.Code,
			Months:          t.Months,
			DiscountPercent: t.DiscountPercent,
			ListAmount:      cyclePrice(monthly, cycleTerm{Months: t.Months}),
			Amount:          amount,
			PerMonth:        math.Round(amount/float64(t.Months)*100) / 100,
		})
		switch t.Code {
		case "SIX_MONTH":
			v := amount
			plan.SixMonthPrice = &v
		case "YEARLY":
			v := amount
			plan.YearlyPrice = &v
		}
	}
}
