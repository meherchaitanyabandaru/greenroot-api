package assistant

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
)

var ist, _ = time.LoadLocation("Asia/Kolkata")

func actor(roles ...string) authctx.Actor { return authctx.Actor{UserID: 7, Roles: roles} }

// fakeAPI stands in for the API's router. It answers like the real
// endpoints, including fields that must never reach the model.
type fakeAPI struct {
	seen   []*http.Request
	status int
}

const sensitiveOrder = `{"id": 11, "order_number": "ORD-1", "order_code": "GR-ORD-1", "order_status": "PENDING",
	"customer_name": "Ravi", "customer_mobile": "9300000000", "total_amount": 1500, "order_date": "%s",
	"delivery_snapshot": {"address_line1": "12 Lake Road", "contact_mobile": "9300000000", "latitude": 17.4},
	"active_dispatch_status": "PENDING", "items": [{"scientific_name": "Mangifera indica", "common_name": "Mango", "quantity": 50}],
	"capabilities": {"can_confirm": true}}`

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.seen = append(f.seen, r)
	if f.status != 0 {
		w.WriteHeader(f.status)
		_, _ = io.WriteString(w, `{"error":{"code":"forbidden","message":"no"}}`)
		return
	}
	today := time.Now().In(ist).Format(time.RFC3339)
	old := time.Now().In(ist).AddDate(0, 0, -3).Format(time.RFC3339)
	order := func(date string) string { return strings.Replace(sensitiveOrder, "%s", date, 1) }
	switch {
	case r.URL.Path == "/api/v1/orders":
		older := strings.Replace(strings.Replace(order(old), "ORD-1", "ORD-0", -1), `"id": 11`, `"id": 10`, 1)
		_, _ = io.WriteString(w, `{"orders": [`+order(today)+`,`+older+`], "pagination": {"total": 2}}`)
	case r.URL.Path == "/api/v1/orders/11":
		_, _ = io.WriteString(w, `{"order": `+order(today)+`}`)
	case r.URL.Path == "/api/v1/dispatches":
		_, _ = io.WriteString(w, `{"dispatches": [{"dispatch_code": "D-1", "order_number": "ORD-1", "dispatch_status": "IN_TRANSIT",
			"driver_name": "Raju", "driver_mobile": "9400000000", "delivery_code": "4821", "destination_address": "12 Lake Road",
			"delivery_latitude": 17.4, "customer_mobile_snapshot": "9300000000"}], "pagination": {"total": 1}}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func env(a authctx.Actor, api http.Handler) *toolEnv {
	return &toolEnv{actor: a, loc: ist, now: time.Now(), api: caller{api: api, auth: "Bearer user-token", remoteAddr: "10.0.0.5:1234", requestID: "req-1"}}
}

func names(specs []toolSpec) []string {
	var out []string
	for _, s := range specs {
		out = append(out, s.Name)
	}
	return out
}

func TestToolsOfferedPerRole(t *testing.T) {
	cases := map[string]struct {
		roles     []string
		want, not []string
		sides     []string
	}{
		"owner":   {[]string{"NURSERY_OWNER", "BUYER"}, []string{"list_orders", "propose_confirm_order", "list_trips"}, nil, []string{"selling", "buying"}},
		"manager": {[]string{"MANAGER"}, []string{"list_orders", "propose_confirm_order"}, nil, []string{"selling"}},
		"buyer":   {[]string{"BUYER"}, []string{"list_orders", "get_delivery_status"}, []string{"propose_confirm_order"}, []string{"buying"}},
		"driver":  {[]string{"DRIVER"}, []string{"list_trips"}, []string{"list_orders", "get_order", "propose_confirm_order", "list_quotations"}, nil},
	}
	for name, c := range cases {
		e := env(actor(c.roles...), &fakeAPI{})
		got := names(toolsFor(e))
		for _, w := range c.want {
			if !slices.Contains(got, w) {
				t.Errorf("%s: missing %s in %v", name, w, got)
			}
		}
		for _, n := range c.not {
			if slices.Contains(got, n) {
				t.Errorf("%s: must not get %s", name, n)
			}
		}
		if c.sides != nil && !slices.Equal(sides(e.actor), c.sides) {
			t.Errorf("%s: sides %v, want %v", name, sides(e.actor), c.sides)
		}
	}
	// An owner who also drives keeps the nursery views only: no "driving" view.
	if got := tripSides(actor("NURSERY_OWNER", "DRIVER")); !slices.Equal(got, []string{"selling", "buying"}) {
		t.Errorf("owner+driver trip sides %v", got)
	}
	if got := tripSides(actor("DRIVER")); !slices.Equal(got, []string{"driving"}) {
		t.Errorf("driver trip sides %v", got)
	}
}

// The model can name any tool or side; the server decides.
func TestModelCannotReachBeyondTheRole(t *testing.T) {
	api := &fakeAPI{}
	for _, c := range []struct {
		roles     []string
		tool, arg string
		want      string
	}{
		{[]string{"DRIVER"}, "list_orders", `{"side":"selling"}`, "not_allowed"},
		{[]string{"BUYER"}, "list_orders", `{"side":"selling"}`, "not_allowed"},
		{[]string{"BUYER"}, "propose_confirm_order", `{"order_number":"ORD-1"}`, "not_allowed"},
		{[]string{"BUYER"}, "drop_tables", `{}`, "unknown_tool"},
		{[]string{"BUYER"}, "list_orders", `not json`, "invalid"},
		{[]string{"MANAGER"}, "list_orders", `{"side":"selling","status":"PAID; DROP"}`, "invalid"},
	} {
		out, _ := json.Marshal(runTool(context.Background(), env(actor(c.roles...), api), c.tool, c.arg))
		if !strings.Contains(string(out), `"error":"`+c.want+`"`) {
			t.Errorf("%v %s %s: %s", c.roles, c.tool, c.arg, out)
		}
	}
	if len(api.seen) != 0 {
		t.Errorf("refused tools still called the API %d times", len(api.seen))
	}
}

func TestToolsCallTheAPIAsTheUserAndPassOnlyAllowedFields(t *testing.T) {
	api := &fakeAPI{}
	e := env(actor("NURSERY_OWNER"), api)
	out, _ := json.Marshal(runTool(context.Background(), e, "list_orders", `{"side":"selling","status":"PENDING","placed_today":true}`))
	got := string(out)
	if !strings.Contains(got, `"total":1`) || !strings.Contains(got, "ORD-1") || strings.Contains(got, "ORD-0") {
		t.Errorf("today's pending orders: %s", got)
	}
	trips, _ := json.Marshal(runTool(context.Background(), e, "list_trips", `{"side":"selling"}`))
	detail, _ := json.Marshal(runTool(context.Background(), e, "get_order", `{"order_number":"ord-1"}`))
	for _, leak := range []string{"9300000000", "9400000000", "Lake Road", "17.4", "4821", "mobile", "latitude", "delivery_code"} {
		for _, body := range []string{got, string(trips), string(detail)} {
			if strings.Contains(body, leak) {
				t.Errorf("%q reached the model: %s", leak, body)
			}
		}
	}
	if !strings.Contains(string(detail), `"plant":"Mango"`) {
		t.Errorf("order detail: %s", detail)
	}
	r := api.seen[0]
	q := r.URL.Query()
	if r.Header.Get("Authorization") != "Bearer user-token" || r.RemoteAddr != "10.0.0.5:1234" || q.Get("order_status") != "PENDING" || q.Has("buying") {
		t.Errorf("call: auth=%q addr=%q query=%v", r.Header.Get("Authorization"), r.RemoteAddr, q)
	}
}

// A driver asking "where do I go" gets the delivery address of their own
// trips; never the delivery code, phone numbers or coordinates.
func TestDriverSeesWhereToDeliverOnly(t *testing.T) {
	out, _ := json.Marshal(runTool(context.Background(), env(actor("DRIVER"), &fakeAPI{}), "list_trips", `{"side":"driving"}`))
	got := string(out)
	if !strings.Contains(got, `"deliver_to":"12 Lake Road"`) {
		t.Errorf("driver trips: %s", got)
	}
	for _, leak := range []string{"4821", "9300000000", "9400000000", "17.4", "mobile", "latitude"} {
		if strings.Contains(got, leak) {
			t.Errorf("%q reached the model: %s", leak, got)
		}
	}
	sell, _ := json.Marshal(runTool(context.Background(), env(actor("NURSERY_OWNER"), &fakeAPI{}), "list_trips", `{"side":"selling"}`))
	if strings.Contains(string(sell), "Lake Road") {
		t.Errorf("the address is for the driver only: %s", sell)
	}
}

func TestBuyingSideAsksForBuyingView(t *testing.T) {
	api := &fakeAPI{}
	runTool(context.Background(), env(actor("BUYER"), api), "list_orders", `{"side":"buying"}`)
	if api.seen[0].URL.Query().Get("buying") != "true" {
		t.Errorf("query %v", api.seen[0].URL.Query())
	}
}

func TestAPIRefusalIsReportedNotHidden(t *testing.T) {
	out, _ := json.Marshal(runTool(context.Background(), env(actor("NURSERY_OWNER"), &fakeAPI{status: 403}), "list_orders", `{"side":"selling"}`))
	if !strings.Contains(string(out), "not_allowed") {
		t.Errorf("got %s", out)
	}
}

// fakeAI scripts the model: the first reply calls a tool, then it answers
// with what the tool returned; "loop" keeps calling tools.
type fakeAI struct {
	calls [][]toolSpec
	msgs  [][]chatMessage
}

func (f *fakeAI) transcribe(context.Context, string, []byte, string, string, string) (string, Usage, error) {
	return "", Usage{}, nil
}
func (f *fakeAI) speak(context.Context, string, string, string) ([]byte, Usage, error) {
	return nil, Usage{}, nil
}
func (f *fakeAI) chat(_ context.Context, _ string, msgs []chatMessage, tools []toolSpec) (chatReply, error) {
	f.calls, f.msgs = append(f.calls, tools), append(f.msgs, msgs)
	last := msgs[len(msgs)-1]
	if tools == nil {
		return chatReply{Content: "final answer without tools"}, nil
	}
	if last.Role == "tool" && !strings.Contains(msgs[1].Content, "loop") {
		return chatReply{Content: "From GreenRoot: " + last.Content}, nil
	}
	return chatReply{ToolCalls: []toolCall{{ID: "c" + time.Now().Format("150405.000000"), Name: "list_orders", Arguments: `{"side":"selling","status":"PENDING"}`}}}, nil
}

func handlerWith(ai aiService, api http.Handler) *Handler {
	h := newHandler(nil, nil, config.AIConfig{Enabled: true, MaxTextChars: 500}, "Asia/Kolkata", api, slog.New(slog.DiscardHandler), ai)
	return h
}

func TestConverseAnswersFromToolData(t *testing.T) {
	ai := &fakeAI{}
	h := handlerWith(ai, &fakeAPI{})
	e := env(actor("NURSERY_OWNER"), h.api)
	reply, used, err := h.converse(context.Background(), e, "req-1", []turn{{"user", "pending orders?"}}, "pending orders?")
	if err != nil || !strings.Contains(reply, `"total":2`) || !slices.Equal(used, []string{"list_orders"}) {
		t.Fatalf("reply=%q used=%v err=%v", reply, used, err)
	}
	sys := ai.msgs[0][0].Content
	if !strings.Contains(sys, "NURSERY_OWNER") || !strings.Contains(sys, "Never guess or invent") || !strings.Contains(sys, "Reply in English") {
		t.Errorf("system prompt: %s", sys)
	}
	if strings.Contains(sys, "%!") {
		t.Errorf("prompt formatting: %s", sys)
	}
}

func TestReplyLanguageFollowsTheQuestion(t *testing.T) {
	ai := &fakeAI{}
	h := handlerWith(ai, &fakeAPI{})
	_, _, _ = h.converse(context.Background(), env(actor("BUYER"), h.api), "req-1", nil, "నా ఆర్డర్ ఎక్కడ ఉంది?")
	if sys := ai.msgs[0][0].Content; !strings.Contains(sys, "Reply in Telugu") || !strings.Contains(sys, "You cannot change anything for this user") {
		t.Errorf("Telugu question from a buyer, prompt: %s", sys)
	}
}

func TestConverseStopsCallingToolsAfterTheLimit(t *testing.T) {
	ai := &fakeAI{}
	h := handlerWith(ai, &fakeAPI{})
	reply, _, err := h.converse(context.Background(), env(actor("NURSERY_OWNER"), h.api), "req-1", []turn{{"user", "loop"}}, "loop")
	if err != nil || reply != "final answer without tools" {
		t.Fatalf("reply=%q err=%v", reply, err)
	}
	if len(ai.calls) != maxToolRounds+1 || ai.calls[maxToolRounds] != nil {
		t.Errorf("model calls %d, last offered tools %v", len(ai.calls), ai.calls[len(ai.calls)-1])
	}
	// Every tool call got an answer (the provider rejects a request otherwise).
	last := ai.msgs[len(ai.msgs)-1]
	asked, answered := 0, 0
	for _, m := range last {
		asked += len(m.ToolCalls)
		if m.Role == "tool" {
			answered++
		}
	}
	if asked != answered {
		t.Errorf("asked %d, answered %d", asked, answered)
	}
}

func TestGatesBeforeAnyAICall(t *testing.T) {
	ai := &fakeAI{}
	h := handlerWith(ai, &fakeAPI{})
	call := func(roles []string, notice string, enabled bool) *httptest.ResponseRecorder {
		h.cfg.Enabled = enabled
		r := httptest.NewRequest(http.MethodPost, "/api/v1/assistant/chat", strings.NewReader(`{"message":"hi"}`))
		r = r.WithContext(authctx.StoreActor(r.Context(), actor(roles...)))
		if notice != "" {
			r.Header.Set("X-Mango-Notice", notice)
		}
		w := httptest.NewRecorder()
		h.Chat(w, r)
		return w
	}
	for _, c := range []struct {
		roles   []string
		notice  string
		enabled bool
		status  int
		code    string
	}{
		{[]string{"ADMIN"}, NoticeVersion, true, 403, "assistant_not_for_admins"},
		{nil, NoticeVersion, true, 403, "assistant_role_not_allowed"},
		{[]string{"BUYER"}, "", true, 428, "ai_notice_required"},
		{[]string{"BUYER"}, "0", true, 428, "ai_notice_required"},
		{[]string{"BUYER"}, NoticeVersion, false, 503, "ai_disabled"},
	} {
		w := call(c.roles, c.notice, c.enabled)
		if w.Code != c.status || !strings.Contains(w.Body.String(), c.code) {
			t.Errorf("%v notice=%q enabled=%v: %d %s", c.roles, c.notice, c.enabled, w.Code, w.Body)
		}
	}
	if len(ai.calls) != 0 {
		t.Errorf("AI was called %d times past a closed gate", len(ai.calls))
	}
}

func TestCostEstimate(t *testing.T) {
	l := ledger{cfg: config.AIConfig{PriceSTTInputPerM: 1.25, PriceSTTOutputPerM: 5, PriceSTTPerMinute: 0.003,
		PriceLLMInputPerM: 0.4, PriceLLMCachedPerM: 0.1, PriceLLMOutputPerM: 1.6, PriceTTSPerMinute: 0.015}}
	for _, c := range []struct {
		kind string
		u    Usage
		want float64
	}{
		{kindSTT, Usage{InputTokens: 1000, OutputTokens: 100}, 0.00125 + 0.0005},
		{kindSTT, Usage{AudioSeconds: 60}, 0.003},
		{kindLLM, Usage{InputTokens: 2000, CachedInputTokens: 1000, OutputTokens: 100}, 0.0004 + 0.0001 + 0.00016},
		{kindTTS, Usage{AudioSeconds: 30}, 0.0075},
	} {
		if got := l.cost(c.kind, c.u); got < c.want-1e-9 || got > c.want+1e-9 {
			t.Errorf("%s %+v: %.7f, want %.7f", c.kind, c.u, got, c.want)
		}
	}
}

func TestTeluguIsDetectedForReadAloud(t *testing.T) {
	if !hasTelugu("మీకు 3 ఆర్డర్లు ఉన్నాయి") || hasTelugu("You have 3 orders") {
		t.Error("Telugu detection")
	}
}

// greenroot-ai refuses "tools": null (seen live: the last round sent it).
func TestChatAlwaysSendsAToolList(t *testing.T) {
	var got map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"content":"ok","tool_calls":[],"usage":{}}`)
	}))
	defer srv.Close()
	if _, err := newHTTPAIService(srv.URL, "t", time.Second).chat(context.Background(), "r", []chatMessage{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if string(got["tools"]) != "[]" {
		t.Errorf("tools sent as %s", got["tools"])
	}
}

// The question day is India time whatever the server's zone: 23:59 IST is
// still the same day, 00:00 IST is the next one and resets the count.
func TestQuestionDayIsIndiaTime(t *testing.T) {
	h := handlerWith(&fakeAI{}, &fakeAPI{})
	for _, c := range []struct {
		utc       string
		day       string
		resetsUTC string
	}{
		{"2026-10-31T18:29:59Z", "2026-10-31", "2026-10-31T18:30:00Z"}, // 23:59:59 IST
		{"2026-10-31T18:30:00Z", "2026-11-01", "2026-11-01T18:30:00Z"}, // 00:00 IST
		{"2026-10-31T02:00:00Z", "2026-10-31", "2026-10-31T18:30:00Z"},
	} {
		now, _ := time.Parse(time.RFC3339, c.utc)
		h.now = func() time.Time { return now }
		day, resets := h.today()
		if day != c.day || resets.UTC().Format(time.RFC3339) != c.resetsUTC {
			t.Errorf("%s: day %s resets %s", c.utc, day, resets.UTC().Format(time.RFC3339))
		}
	}
}

func TestQuotaArithmetic(t *testing.T) {
	q := quota(7, 5, time.Time{})
	if q.Left != 0 || limitReached(q).Code != "ai_daily_questions_used" || !strings.Contains(limitReached(q).Message, "5 questions") {
		t.Errorf("%+v", q)
	}
	if quota(2, 5, time.Time{}).Left != 3 {
		t.Error("left")
	}
}
