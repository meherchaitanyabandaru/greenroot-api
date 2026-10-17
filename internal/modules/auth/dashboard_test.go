package auth

import "testing"

// The dashboard buckets must cover the real state machines; the old mapping
// used status names that do not exist, so owners saw zeros.
func TestDashboardStatusMapping(t *testing.T) {
	var o OrderMetrics
	fillOrders(&o, map[string]int{"PENDING": 48, "CONFIRMED": 18, "LOADING": 3, "LOADED": 20, "PARTIALLY_FULFILLED": 5, "COMPLETED": 44, "CANCELLED": 44})
	if o != (OrderMetrics{Total: 182, Pending: 48, Confirmed: 46, Delivered: 44, Cancelled: 44}) {
		t.Fatalf("orders = %+v", o)
	}
	var q QuoteMetrics
	fillQuotes(&q, map[string]int{"CUSTOMER_DRAFT": 178, "EXPIRED": 26, "CONVERTED": 16, "CUSTOMER_REJECTED": 15, "INTERNAL_DRAFT": 12, "CUSTOMER_SENT": 9, "CUSTOMER_ACCEPTED": 2})
	if q != (QuoteMetrics{Total: 258, Pending: 9, Approved: 18, Rejected: 15}) {
		t.Fatalf("quotes = %+v", q)
	}
}
