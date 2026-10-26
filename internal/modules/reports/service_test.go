package reports

import (
	"context"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
)

func vals(kv ...string) url.Values {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

func TestParseRequest_TypeDatesFormatPaging(t *testing.T) {
	if _, err := ParseRequest("payments", vals("from", "2026-01-01", "to", "2026-01-31")); err != ErrInvalidType {
		t.Fatalf("unknown type: want ErrInvalidType, got %v", err)
	}
	bad := [][]string{
		{"to", "2026-01-31"},                       // no from
		{"from", "2026-01-31", "to", "2026-01-01"}, // backwards
		{"from", "01-01-2026", "to", "2026-01-31"}, // wrong format
		{"from", "2023-01-01", "to", "2026-01-02"}, // over 3 years
	}
	for _, kv := range bad {
		if _, err := ParseRequest(TypeOrders, vals(kv...)); err != ErrInvalidRange {
			t.Fatalf("%v: want ErrInvalidRange, got %v", kv, err)
		}
	}
	// exactly 3 years is allowed; one day is allowed
	for _, kv := range [][]string{{"from", "2023-01-01", "to", "2026-01-01"}, {"from", "2026-02-10", "to", "2026-02-10"}} {
		if _, err := ParseRequest(TypeOrders, vals(kv...)); err != nil {
			t.Fatalf("%v: want ok, got %v", kv, err)
		}
	}
	if _, err := ParseRequest(TypeOrders, vals("from", "2026-01-01", "to", "2026-01-02", "format", "xlsx")); err != ErrInvalidFormat {
		t.Fatalf("xlsx: want ErrInvalidFormat, got %v", err)
	}
	req, err := ParseRequest(TypeDeliveries, vals("from", "2026-01-01", "to", "2026-01-31", "format", "csv", "page", "0", "per_page", "500", "view", "buying"))
	if err != nil || !req.CSV || req.Page != 1 || req.PerPage != 20 || !req.Buying {
		t.Fatalf("defaults/flags wrong: %+v %v", req, err)
	}
	// the range is whole India days: to is inclusive
	if got := req.ToExcl.Sub(req.From).Hours(); got != 31*24 {
		t.Fatalf("Jan 1..31 should span 31 days, got %v h", got)
	}
	if req.From.Format("2006-01-02T15:04Z07:00") != "2026-01-01T00:00+05:30" {
		t.Fatalf("from must be 00:00 IST, got %s", req.From)
	}
}

type fakeStore struct {
	owned, member int64
}

func (f fakeStore) OwnedNurseryID(context.Context, int64) (int64, error)  { return f.owned, nil }
func (f fakeStore) MemberNurseryID(context.Context, int64) (int64, error) { return f.member, nil }
func (fakeStore) Summary(context.Context, Request, Scope) (Summary, error) {
	return Summary{Total: 45}, nil
}
func (fakeStore) Rows(context.Context, Request, Scope, int, int) ([]Row, error) { return []Row{}, nil }
func (fakeStore) StreamCSV(context.Context, Request, Scope, io.Writer) error    { return nil }

func actor(roles ...string) authctx.ActorContext {
	return authctx.ActorContext{UserID: 7, Roles: roles}
}

func TestResolveScope_EachRoleSeesOnlyTheirSlice(t *testing.T) {
	ctx := context.Background()
	s := NewService(fakeStore{owned: 52, member: 61})
	orders := Request{Type: TypeOrders, Nursery: 99}
	cases := []struct {
		name    string
		actor   authctx.ActorContext
		req     Request
		kind    string
		nursery int64
	}{
		{"admin sees all, may filter a nursery", actor("ADMIN"), orders, ScopeAll, 99},
		{"owner: own nursery, request nursery_id ignored", actor("NURSERY_OWNER", "BUYER"), orders, ScopeNursery, 52},
		{"manager: own work in their nursery", actor("MANAGER"), orders, ScopeManager, 61},
		{"driver: own trips", actor("DRIVER"), Request{Type: TypeDeliveries}, ScopeDriver, 0},
		{"customer: own purchases", actor("BUYER"), orders, ScopeBuyer, 0},
		{"owner buying view", actor("NURSERY_OWNER"), Request{Type: TypeOrders, Buying: true}, ScopeBuyer, 0},
	}
	for _, c := range cases {
		sc, err := s.ResolveScope(ctx, c.actor, c.req)
		if err != nil || sc.Kind != c.kind || sc.NurseryID != c.nursery || sc.UserID != 7 {
			t.Fatalf("%s: got %+v %v", c.name, sc, err)
		}
	}
	if _, err := s.ResolveScope(ctx, actor("DRIVER"), orders); err != ErrForbidden {
		t.Fatalf("driver orders report: want ErrForbidden, got %v", err)
	}
	none := NewService(fakeStore{})
	if _, err := none.ResolveScope(ctx, actor("NURSERY_OWNER"), orders); err != ErrForbidden {
		t.Fatalf("owner without a nursery: want ErrForbidden, got %v", err)
	}
	if _, err := none.ResolveScope(ctx, actor("MANAGER"), orders); err != ErrForbidden {
		t.Fatalf("manager without a nursery: want ErrForbidden, got %v", err)
	}
}

func TestBuild_ScopeFiltersAreParameters(t *testing.T) {
	req, _ := ParseRequest(TypeOrders, vals("from", "2026-01-01", "to", "2026-01-31"))
	q := build(req, Scope{Kind: ScopeManager, UserID: 7, NurseryID: 52})
	for _, want := range []string{"o.seller_nursery_id = $1", "o.assigned_manager_user_id = $2 OR o.created_by_user_id = $2", ">= $3", "< $4"} {
		if !strings.Contains(q.where, want) {
			t.Fatalf("manager orders where missing %q: %s", want, q.where)
		}
	}
	if q.args[0] != int64(52) || q.args[1] != int64(7) {
		t.Fatalf("args: %v", q.args)
	}
	quo, _ := ParseRequest(TypeQuotations, vals("from", "2026-01-01", "to", "2026-01-31"))
	if w := build(quo, Scope{Kind: ScopeBuyer, UserID: 7}).where; !strings.Contains(w, "NOT IN ('INTERNAL_DRAFT', 'CUSTOMER_DRAFT')") || !strings.Contains(w, "x.customer_user_id = $1") {
		t.Fatalf("quotations must skip drafts and scope to the customer: %s", w)
	}
	del, _ := ParseRequest(TypeDeliveries, vals("from", "2026-01-01", "to", "2026-01-31"))
	if w := build(del, Scope{Kind: ScopeDriver, UserID: 7}).where; !strings.Contains(w, "d.driver_user_id = $1") {
		t.Fatalf("driver deliveries: %s", w)
	}
	// Every parameter must appear in the SQL (Postgres rejects untyped extras).
	for _, sc := range []Scope{{Kind: ScopeAll}, {Kind: ScopeAll, NurseryID: 3}, {Kind: ScopeNursery, NurseryID: 3}} {
		q := build(req, sc)
		if strings.Contains(q.where, "seller_nursery_id") != (sc.NurseryID > 0) || strings.Count(q.where, "$") != len(q.args) {
			t.Fatalf("%+v: %s %v", sc, q.where, q.args)
		}
	}
}

func TestMaskMobileAndCSVSafety(t *testing.T) {
	for in, want := range map[string]string{"9876543210": "98xxxxx210", "+919876543210": "+9xxxxxxxx210", "123": "xxx", "": ""} {
		if got := MaskMobile(in); got != want {
			t.Fatalf("MaskMobile(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"=HYPERLINK(1)": "'=HYPERLINK(1)", "+1": "'+1", "-2": "'-2", "@SUM": "'@SUM", "Ravi": "Ravi"} {
		if got := csvSafe(in); got != want {
			t.Fatalf("csvSafe(%q) = %q, want %q", in, got, want)
		}
	}
	amt, paid := 1500.0, 0.0
	row := Row{Reference: "ORD-1", Date: "2026-01-02", Party: "=cmd", Mobile: "98xxxxx210", Status: "COMPLETED", Amount: &amt, Paid: &paid}
	got := strings.Join(csvRecord(TypeOrders, false, row), "|")
	if got != "ORD-1|2026-01-02|'=cmd|98xxxxx210|COMPLETED|1500.00|0.00" {
		t.Fatalf("orders csv record: %s", got)
	}
	if h := strings.Join(csvHeader(TypeOrders, true), "|"); h != "Reference|Date|Nursery|Status|Amount (INR)|Paid (INR)" {
		t.Fatalf("buyer header has no mobile, names the nursery: %s", h)
	}
}

func TestReport_Pagination(t *testing.T) {
	req, _ := ParseRequest(TypeOrders, vals("from", "2026-01-01", "to", "2026-01-31", "page", "3", "per_page", "20"))
	rep, err := NewService(fakeStore{}).Report(context.Background(), req, Scope{Kind: ScopeAll})
	if err != nil || rep.Pagination.TotalPages != 3 || rep.Pagination.Page != 3 || rep.To != "2026-01-31" || rep.Rows == nil {
		t.Fatalf("45 rows / 20 = 3 pages, to inclusive: %+v %v", rep, err)
	}
}
