package reports

import (
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

// Report types.
const (
	TypeOrders     = "orders"
	TypeQuotations = "quotations"
	TypeDeliveries = "deliveries"
)

// Scope kinds: whose rows a report reads.
const (
	ScopeAll     = "all"     // admin
	ScopeNursery = "nursery" // owner: the whole nursery
	ScopeManager = "manager" // manager: assigned to or created by them, in their nursery
	ScopeBuyer   = "buyer"   // customer: their own purchases
	ScopeDriver  = "driver"  // driver: their own trips
)

// MaxRangeYears is the longest date range one request may ask for.
const MaxRangeYears = 3

// Request is a validated report request. From is the first day, ToExcl the
// day after the last one, both at 00:00 India time.
type Request struct {
	Type    string
	From    time.Time
	ToExcl  time.Time
	CSV     bool
	Page    int
	PerPage int
	Buying  bool  // ?view=buying: the caller's own purchases
	Nursery int64 // admin only: one nursery
}

// Scope is who is asking and which rows they may read.
type Scope struct {
	Kind      string
	UserID    int64
	NurseryID int64 // nursery/manager scopes, or an admin's optional filter
}

// Summary holds the counts and amounts for one report. Fields not used by a
// report type stay zero and are left out of the JSON.
type Summary struct {
	Total int64 `json:"total"`

	// orders
	Delivered  int64   `json:"delivered,omitempty"`  // COMPLETED: delivery confirmed
	Cancelled  int64   `json:"cancelled,omitempty"`  // orders and deliveries
	Sales      float64 `json:"sales,omitempty"`      // value of delivered (COMPLETED) orders
	OpenValue  float64 `json:"open_value,omitempty"` // value of orders not yet delivered or cancelled
	PaidAmount float64 `json:"paid,omitempty"`       // successful payments on these orders

	// quotations
	Accepted        int64   `json:"accepted,omitempty"` // CUSTOMER_ACCEPTED or CONVERTED
	Pending         int64   `json:"pending,omitempty"`  // CUSTOMER_SENT
	RejectedExpired int64   `json:"rejected_expired,omitempty"`
	AcceptedValue   float64 `json:"accepted_value,omitempty"`

	// deliveries
	InProgress int64 `json:"in_progress,omitempty"` // PENDING..IN_TRANSIT
}

// Row is one record. Party is the other side: the customer for a seller, the
// nursery for a buyer. Mobile is always masked.
type Row struct {
	Reference string   `json:"reference"`
	Date      string   `json:"date"` // YYYY-MM-DD, India time
	Party     string   `json:"party"`
	Mobile    string   `json:"mobile,omitempty"`
	Status    string   `json:"status"`
	Amount    *float64 `json:"amount,omitempty"`
	Paid      *float64 `json:"paid,omitempty"`      // orders
	OrderRef  string   `json:"order_ref,omitempty"` // deliveries
}

// Report is the JSON response.
type Report struct {
	Type       string              `json:"type"`
	From       string              `json:"from"`
	To         string              `json:"to"`
	Summary    Summary             `json:"summary"`
	Rows       []Row               `json:"rows"`
	Pagination response.Pagination `json:"pagination"`
}
