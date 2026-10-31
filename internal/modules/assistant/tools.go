package assistant

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
)

// caller runs API endpoints as the signed-in user: the same token, so the
// same permission, nursery and state checks as the app.
type caller struct {
	api        http.Handler
	auth       string
	remoteAddr string
	userAgent  string
	requestID  string
}

func (c caller) call(ctx context.Context, method, path string, query url.Values, body any) (int, []byte) {
	// A fresh context: the outer request's router state must not leak in.
	inner, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer context.AfterFunc(ctx, cancel)()
	target := "/api/v1" + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequestWithContext(inner, method, target, reader)
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("X-Request-Id", c.requestID)
	req.RemoteAddr = c.remoteAddr
	rec := httptest.NewRecorder()
	c.api.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// toolEnv is one chat turn: who asks, how to reach the API, and the change
// Mango proposed in this turn (at most one).
type toolEnv struct {
	actor    authctx.Actor
	api      caller
	db       *sql.DB
	loc      *time.Location
	now      time.Time
	proposal *Proposal
}

// Proposal is a change waiting for the user's Confirm.
type Proposal struct {
	ID        int64     `json:"id"`
	Summary   string    `json:"summary"`
	ExpiresAt time.Time `json:"expires_at"`
}

const proposalTTL = 10 * time.Minute

type tool struct {
	spec    func(env *toolEnv) toolSpec
	allowed func(a authctx.Actor) bool
	run     func(ctx context.Context, env *toolEnv, args map[string]any) any
}

var (
	orderStatuses     = []string{"PENDING", "CONFIRMED", "LOADING", "LOADED", "PARTIALLY_FULFILLED", "COMPLETED", "CANCELLED"}
	quotationStatuses = []string{"CUSTOMER_REQUESTED", "INTERNAL_DRAFT", "CUSTOMER_DRAFT", "CUSTOMER_SENT", "CUSTOMER_ACCEPTED", "CUSTOMER_REJECTED", "CONVERTED", "EXPIRED", "REQUEST_DECLINED", "REQUEST_CANCELLED"}
	tripStatuses      = []string{"PENDING", "ACCEPTED", "DISPATCHED", "IN_TRANSIT", "DELIVERED", "CANCELLED"}
)

func canSell(a authctx.Actor) bool { return a.HasRole("NURSERY_OWNER", "MANAGER") }
func canBuy(a authctx.Actor) bool  { return a.HasRole("BUYER", "NURSERY_OWNER") }

// sides lists the views of orders/quotations this user has.
func sides(a authctx.Actor) []string {
	var out []string
	if canSell(a) {
		out = append(out, "selling")
	}
	if canBuy(a) {
		out = append(out, "buying")
	}
	return out
}

func toolError(code, message string) map[string]string {
	return map[string]string{"error": code, "message": message}
}

// apiFailure turns an endpoint's refusal into words for the model.
func apiFailure(status int) map[string]string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return toolError("not_allowed", "this account is not allowed to see that")
	case http.StatusNotFound:
		return toolError("not_found", "nothing found")
	}
	return toolError("unavailable", "GreenRoot could not load this right now")
}

func str(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func upperArg(args map[string]any, key string) string {
	v := strings.ToUpper(str(args, key))
	if len(v) > 40 {
		v = v[:40]
	}
	return v
}

func sideParam(allowed []string) map[string]any {
	return map[string]any{"type": "string", "enum": allowed,
		"description": "selling = orders customers placed with the user's nursery; buying = orders the user placed"}
}

func objectSchema(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

// ── what the model sees (allowlisted fields; no phone numbers, addresses,
// coordinates, delivery codes or payment details) ─────────────────────────

type orderRow struct {
	ID                   int64     `json:"id"`
	OrderNumber          string    `json:"order_number"`
	OrderCode            string    `json:"order_code"`
	Status               string    `json:"order_status"`
	CustomerName         *string   `json:"customer_name"`
	BuyerName            *string   `json:"buyer_name"`
	SellerNursery        *string   `json:"seller_nursery"`
	NurseryName          *string   `json:"nursery_name"`
	Total                float64   `json:"total_amount"`
	OrderDate            time.Time `json:"order_date"`
	ActiveDispatchStatus *string   `json:"active_dispatch_status"`
	Items                []struct {
		CommonName      *string  `json:"common_name"`
		ScientificName  string   `json:"scientific_name"`
		CustomPlantName *string  `json:"custom_plant_name"`
		SizeName        *string  `json:"size_name"`
		Quantity        float64  `json:"quantity"`
		LoadedQuantity  *float64 `json:"loaded_quantity"`
	} `json:"items"`
	Capabilities *struct {
		CanConfirm bool `json:"can_confirm"`
	} `json:"capabilities"`
}

type itemOut struct {
	Plant    string   `json:"plant"`
	Size     string   `json:"size,omitempty"`
	Quantity float64  `json:"quantity"`
	Loaded   *float64 `json:"loaded_quantity,omitempty"`
}

type orderOut struct {
	OrderNumber string    `json:"order_number"`
	Status      string    `json:"status"`
	Customer    string    `json:"customer,omitempty"`
	Nursery     string    `json:"nursery,omitempty"`
	TotalRupees float64   `json:"total_rupees"`
	OrderDate   string    `json:"order_date"`
	Delivery    string    `json:"delivery_status,omitempty"`
	Items       []itemOut `json:"items,omitempty"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func firstOf(values ...*string) string {
	for _, v := range values {
		if v != nil && strings.TrimSpace(*v) != "" {
			return *v
		}
	}
	return ""
}

func (o orderRow) brief(side string, loc *time.Location, withItems bool) orderOut {
	out := orderOut{OrderNumber: o.OrderNumber, Status: o.Status, TotalRupees: o.Total,
		OrderDate: o.OrderDate.In(loc).Format("2006-01-02"), Delivery: deref(o.ActiveDispatchStatus)}
	if side == "selling" {
		out.Customer = firstOf(o.CustomerName, o.BuyerName)
	} else {
		out.Nursery = firstOf(o.SellerNursery, o.NurseryName)
	}
	if withItems {
		for i, it := range o.Items {
			if i == 30 {
				break
			}
			name := firstOf(it.CustomPlantName, it.CommonName)
			if name == "" {
				name = it.ScientificName
			}
			out.Items = append(out.Items, itemOut{Plant: name, Size: deref(it.SizeName), Quantity: it.Quantity, Loaded: it.LoadedQuantity})
		}
	}
	return out
}

type quotationRow struct {
	ID            int64      `json:"id"`
	QuotationCode string     `json:"quotation_code"`
	Status        string     `json:"status"`
	Total         float64    `json:"total_amount"`
	RecipientName *string    `json:"recipient_name"`
	NurseryName   *string    `json:"nursery_name"`
	ValidUntil    *time.Time `json:"valid_until"`
	SentAt        *time.Time `json:"sent_at"`
	IsRequest     bool       `json:"is_request"`
	CreatedAt     time.Time  `json:"created_at"`
}

func (q quotationRow) brief(side string, loc *time.Location) map[string]any {
	out := map[string]any{"quotation_code": q.QuotationCode, "status": q.Status, "total_rupees": q.Total,
		"created_on": q.CreatedAt.In(loc).Format("2006-01-02"), "asked_by_customer": q.IsRequest}
	if side == "selling" {
		out["customer"] = deref(q.RecipientName)
	} else {
		out["nursery"] = deref(q.NurseryName)
	}
	if q.ValidUntil != nil {
		out["valid_until"] = q.ValidUntil.In(loc).Format("2006-01-02")
	}
	return out
}

type tripRow struct {
	DispatchNumber *string    `json:"dispatch_number"`
	DispatchCode   string     `json:"dispatch_code"`
	OrderNumber    *string    `json:"order_number"`
	Status         string     `json:"dispatch_status"`
	SellerNursery  *string    `json:"seller_nursery_name"`
	Customer       *string    `json:"customer_name_snapshot"`
	VehicleNumber  *string    `json:"vehicle_number"`
	DriverName     *string    `json:"driver_name"`
	DispatchDate   *time.Time `json:"dispatch_date"`
	DeliveryDate   *time.Time `json:"delivery_date"`
	TripStartedAt  *time.Time `json:"trip_started_at"`
	CompletedAt    *time.Time `json:"completed_at"`
}

func (t tripRow) brief(loc *time.Location) map[string]any {
	out := map[string]any{"trip": firstOf(t.DispatchNumber, &t.DispatchCode), "order_number": deref(t.OrderNumber),
		"status": t.Status, "nursery": deref(t.SellerNursery), "customer": deref(t.Customer),
		"lorry": deref(t.VehicleNumber), "driver": deref(t.DriverName)}
	for key, at := range map[string]*time.Time{"dispatch_date": t.DispatchDate, "expected_delivery": t.DeliveryDate, "started_at": t.TripStartedAt, "completed_at": t.CompletedAt} {
		if at != nil {
			out[key] = at.In(loc).Format("2006-01-02 15:04")
		}
	}
	return out
}

// ── lookups ──────────────────────────────────────────────────────────────

// findOrder resolves an order number the user said to an order they can
// see, trying each of their sides. The endpoint does the access check.
func (env *toolEnv) findOrder(ctx context.Context, number string, onlySide string) (orderRow, string, map[string]string) {
	if number == "" {
		return orderRow{}, "", toolError("missing", "which order number?")
	}
	for _, side := range sides(env.actor) {
		if onlySide != "" && side != onlySide {
			continue
		}
		q := url.Values{"search": {number}, "per_page": {"10"}}
		if side == "buying" {
			q.Set("buying", "true")
		}
		status, body := env.api.call(ctx, http.MethodGet, "/orders", q, nil)
		if status != http.StatusOK {
			continue
		}
		var list struct {
			Orders []orderRow `json:"orders"`
		}
		_ = json.Unmarshal(body, &list)
		for _, o := range list.Orders {
			if strings.EqualFold(o.OrderNumber, number) || strings.EqualFold(o.OrderCode, number) {
				status, body = env.api.call(ctx, http.MethodGet, "/orders/"+strconv.FormatInt(o.ID, 10), nil, nil)
				if status != http.StatusOK {
					return orderRow{}, "", apiFailure(status)
				}
				var detail struct {
					Order orderRow `json:"order"`
				}
				if json.Unmarshal(body, &detail) != nil {
					return orderRow{}, "", apiFailure(0)
				}
				return detail.Order, side, nil
			}
		}
	}
	return orderRow{}, "", toolError("not_found", "no order with that number is visible to this account")
}

// ── the tools ────────────────────────────────────────────────────────────

var tools = []tool{
	{
		spec: func(env *toolEnv) toolSpec {
			return toolSpec{Name: "list_orders", Description: "List the user's orders. Use for questions like 'pending orders today' or 'my orders'. Returns real counts.",
				Parameters: objectSchema(map[string]any{
					"side":         sideParam(sides(env.actor)),
					"status":       map[string]any{"type": "string", "enum": orderStatuses},
					"placed_today": map[string]any{"type": "boolean", "description": "only orders dated today"},
					"search":       map[string]any{"type": "string", "description": "customer, nursery or order number text"},
				}, "side")}
		},
		allowed: func(a authctx.Actor) bool { return len(sides(a)) > 0 },
		run: func(ctx context.Context, env *toolEnv, args map[string]any) any {
			side, status := str(args, "side"), upperArg(args, "status")
			if !slices.Contains(sides(env.actor), side) {
				return toolError("not_allowed", "this account has no "+side+" orders view")
			}
			q := url.Values{"per_page": {"100"}}
			if side == "buying" {
				q.Set("buying", "true")
			}
			if status != "" {
				if !slices.Contains(orderStatuses, status) {
					return toolError("invalid", "unknown order status")
				}
				q.Set("order_status", status)
			}
			if s := str(args, "search"); s != "" {
				q.Set("search", s[:min(len(s), 40)])
			}
			code, body := env.api.call(ctx, http.MethodGet, "/orders", q, nil)
			if code != http.StatusOK {
				return apiFailure(code)
			}
			var list struct {
				Orders     []orderRow `json:"orders"`
				Pagination struct {
					Total int `json:"total"`
				} `json:"pagination"`
			}
			if json.Unmarshal(body, &list) != nil {
				return apiFailure(0)
			}
			today, _ := args["placed_today"].(bool)
			date := env.now.In(env.loc).Format("2006-01-02")
			total := list.Pagination.Total
			var rows []orderOut
			for _, o := range list.Orders {
				b := o.brief(side, env.loc, false)
				if today && b.OrderDate != date {
					continue
				}
				rows = append(rows, b)
			}
			if today {
				total = len(rows)
			}
			out := map[string]any{"side": side, "status_filter": status, "placed_today_only": today, "today": date, "total": total, "orders": rows[:min(len(rows), 10)]}
			if len(rows) > 10 || list.Pagination.Total > len(list.Orders) {
				out["note"] = "only the first orders are listed; the total is exact unless incomplete is true"
			}
			if today && list.Pagination.Total > len(list.Orders) {
				out["incomplete"] = true
			}
			return out
		},
	},
	{
		spec: func(env *toolEnv) toolSpec {
			return toolSpec{Name: "get_order", Description: "One order by its number: status, items, delivery status.",
				Parameters: objectSchema(map[string]any{"order_number": map[string]any{"type": "string"}}, "order_number")}
		},
		allowed: func(a authctx.Actor) bool { return len(sides(a)) > 0 },
		run: func(ctx context.Context, env *toolEnv, args map[string]any) any {
			o, side, fail := env.findOrder(ctx, upperArg(args, "order_number"), "")
			if fail != nil {
				return fail
			}
			return o.brief(side, env.loc, true)
		},
	},
	{
		spec: func(env *toolEnv) toolSpec {
			return toolSpec{Name: "list_quotations", Description: "List the user's quotations (price offers) and customers' price requests.",
				Parameters: objectSchema(map[string]any{
					"side":   sideParam(sides(env.actor)),
					"status": map[string]any{"type": "string", "enum": quotationStatuses},
					"search": map[string]any{"type": "string"},
				}, "side")}
		},
		allowed: func(a authctx.Actor) bool { return len(sides(a)) > 0 },
		run: func(ctx context.Context, env *toolEnv, args map[string]any) any {
			side, status := str(args, "side"), upperArg(args, "status")
			if !slices.Contains(sides(env.actor), side) {
				return toolError("not_allowed", "this account has no "+side+" quotations view")
			}
			q := url.Values{"per_page": {"50"}}
			if side == "buying" {
				q.Set("buying", "true")
			}
			if status != "" {
				if !slices.Contains(quotationStatuses, status) {
					return toolError("invalid", "unknown quotation status")
				}
				q.Set("status", status)
			}
			if s := str(args, "search"); s != "" {
				q.Set("search", s[:min(len(s), 40)])
			}
			code, body := env.api.call(ctx, http.MethodGet, "/quotations", q, nil)
			if code != http.StatusOK {
				return apiFailure(code)
			}
			var list struct {
				Quotations []quotationRow `json:"quotations"`
				Pagination struct {
					Total int `json:"total"`
				} `json:"pagination"`
			}
			if json.Unmarshal(body, &list) != nil {
				return apiFailure(0)
			}
			rows := make([]map[string]any, 0, 10)
			for _, qt := range list.Quotations[:min(len(list.Quotations), 10)] {
				rows = append(rows, qt.brief(side, env.loc))
			}
			return map[string]any{"side": side, "status_filter": status, "total": list.Pagination.Total, "quotations": rows}
		},
	},
	{
		spec: func(env *toolEnv) toolSpec {
			return toolSpec{Name: "get_quotation", Description: "One quotation by its code.",
				Parameters: objectSchema(map[string]any{"quotation_code": map[string]any{"type": "string"}}, "quotation_code")}
		},
		allowed: func(a authctx.Actor) bool { return len(sides(a)) > 0 },
		run: func(ctx context.Context, env *toolEnv, args map[string]any) any {
			want := upperArg(args, "quotation_code")
			if want == "" {
				return toolError("missing", "which quotation code?")
			}
			for _, side := range sides(env.actor) {
				q := url.Values{"search": {want}, "per_page": {"10"}}
				if side == "buying" {
					q.Set("buying", "true")
				}
				status, body := env.api.call(ctx, http.MethodGet, "/quotations", q, nil)
				if status != http.StatusOK {
					continue
				}
				var list struct {
					Quotations []quotationRow `json:"quotations"`
				}
				_ = json.Unmarshal(body, &list)
				for _, qt := range list.Quotations {
					if strings.EqualFold(qt.QuotationCode, want) {
						return qt.brief(side, env.loc)
					}
				}
			}
			return toolError("not_found", "no quotation with that code is visible to this account")
		},
	},
	{
		spec: func(env *toolEnv) toolSpec {
			return toolSpec{Name: "list_trips", Description: "List delivery trips (dispatches): on the way, waiting for a driver, delivered.",
				Parameters: objectSchema(map[string]any{
					"side":   sideParam(sides(env.actor)),
					"status": map[string]any{"type": "string", "enum": tripStatuses},
				}, "side")}
		},
		allowed: func(a authctx.Actor) bool { return len(sides(a)) > 0 },
		run: func(ctx context.Context, env *toolEnv, args map[string]any) any {
			side, status := str(args, "side"), upperArg(args, "status")
			if !slices.Contains(sides(env.actor), side) {
				return toolError("not_allowed", "this account has no "+side+" trips view")
			}
			q := url.Values{"per_page": {"50"}}
			if side == "buying" {
				q.Set("buying", "true")
			}
			if status != "" {
				if !slices.Contains(tripStatuses, status) {
					return toolError("invalid", "unknown trip status")
				}
				q.Set("dispatch_status", status)
			}
			code, body := env.api.call(ctx, http.MethodGet, "/dispatches", q, nil)
			if code != http.StatusOK {
				return apiFailure(code)
			}
			var list struct {
				Dispatches []tripRow `json:"dispatches"`
				Pagination struct {
					Total int `json:"total"`
				} `json:"pagination"`
			}
			if json.Unmarshal(body, &list) != nil {
				return apiFailure(0)
			}
			rows := make([]map[string]any, 0, 10)
			for _, t := range list.Dispatches[:min(len(list.Dispatches), 10)] {
				rows = append(rows, t.brief(env.loc))
			}
			return map[string]any{"side": side, "status_filter": status, "total": list.Pagination.Total, "trips": rows}
		},
	},
	{
		spec: func(env *toolEnv) toolSpec {
			return toolSpec{Name: "get_delivery_status", Description: "Where the delivery of one order is: its trips, lorry, driver and status.",
				Parameters: objectSchema(map[string]any{"order_number": map[string]any{"type": "string"}}, "order_number")}
		},
		allowed: func(a authctx.Actor) bool { return len(sides(a)) > 0 },
		run: func(ctx context.Context, env *toolEnv, args map[string]any) any {
			o, side, fail := env.findOrder(ctx, upperArg(args, "order_number"), "")
			if fail != nil {
				return fail
			}
			status, body := env.api.call(ctx, http.MethodGet, fmt.Sprintf("/orders/%d/dispatches", o.ID), nil, nil)
			if status != http.StatusOK {
				return apiFailure(status)
			}
			var list struct {
				Dispatches []tripRow `json:"dispatches"`
			}
			if json.Unmarshal(body, &list) != nil {
				return apiFailure(0)
			}
			trips := make([]map[string]any, 0, len(list.Dispatches))
			for _, t := range list.Dispatches[:min(len(list.Dispatches), 5)] {
				trips = append(trips, t.brief(env.loc))
			}
			return map[string]any{"order": o.brief(side, env.loc, false), "trips": trips}
		},
	},
	{
		spec: func(env *toolEnv) toolSpec {
			return toolSpec{Name: "propose_confirm_order", Description: "Prepare confirming a PENDING order the user's nursery received. Does NOT confirm: the user must tap Confirm in the app.",
				Parameters: objectSchema(map[string]any{"order_number": map[string]any{"type": "string"}}, "order_number")}
		},
		allowed: canSell,
		run: func(ctx context.Context, env *toolEnv, args map[string]any) any {
			if !canSell(env.actor) {
				return toolError("not_allowed", "only a nursery owner or manager confirms orders")
			}
			if env.proposal != nil {
				return toolError("one_at_a_time", "one change at a time: the user must answer the first one")
			}
			o, _, fail := env.findOrder(ctx, upperArg(args, "order_number"), "selling")
			if fail != nil {
				return fail
			}
			if o.Capabilities == nil || !o.Capabilities.CanConfirm {
				return toolError("not_possible", fmt.Sprintf("order %s is %s; only a PENDING order can be confirmed, by its nursery", o.OrderNumber, o.Status))
			}
			summary := fmt.Sprintf("Confirm order %s from %s (₹%.0f)", o.OrderNumber, firstOf(o.CustomerName, o.BuyerName, ptr("the customer")), o.Total)
			p := Proposal{Summary: summary}
			err := env.db.QueryRowContext(ctx, `
				INSERT INTO public.assistant_actions (user_id, action_type, target_id, summary, expires_at)
				VALUES ($1, 'CONFIRM_ORDER', $2, $3, now() + $4 * interval '1 second')
				RETURNING assistant_action_id, expires_at`, env.actor.UserID, o.ID, summary, int(proposalTTL.Seconds())).Scan(&p.ID, &p.ExpiresAt)
			if err != nil {
				return apiFailure(0)
			}
			env.proposal = &p
			return map[string]any{"status": "awaiting_user_confirmation", "summary": summary,
				"instruction": "Tell the user what will happen and that they must tap Confirm below. It is NOT done yet."}
		},
	},
}

func ptr(s string) *string { return &s }

func toolsFor(env *toolEnv) []toolSpec {
	var out []toolSpec
	for _, t := range tools {
		if t.allowed(env.actor) {
			out = append(out, t.spec(env))
		}
	}
	return out
}

// runTool checks again that the tool is offered to this user: the model
// may name any tool.
func runTool(ctx context.Context, env *toolEnv, name, rawArgs string) any {
	for _, t := range tools {
		if t.spec(env).Name != name {
			continue
		}
		if !t.allowed(env.actor) {
			return toolError("not_allowed", "this account cannot use "+name)
		}
		args := map[string]any{}
		if rawArgs != "" && json.Unmarshal([]byte(rawArgs), &args) != nil {
			return toolError("invalid", "arguments were not valid JSON")
		}
		return t.run(ctx, env, args)
	}
	return toolError("unknown_tool", "no such tool")
}
