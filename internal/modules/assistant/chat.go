package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// maxToolRounds bounds the model calls per question (cost and latency): up
// to this many rounds may use tools, then one last call must answer.
const maxToolRounds = 2

const systemPrompt = `You are Mango, the helper inside GreenRoot, a mobile app for plant nurseries in India. Users speak Telugu, English or a mix.
Today is %s (India time). The user's roles: %s.

Rules:
- Reply in %s. Be short: at most 3 short sentences or a short list. No tables, no markdown headings.
- Facts about the user's orders, quotations, trips and deliveries come ONLY from tool results in this conversation. Never guess or invent counts, names, amounts, dates or statuses. If a tool returns an error or nothing, say so plainly.
- Give numbers exactly as the tool returned them. Money is in rupees (₹).
- %s
- Do not promise plant health, payment outcomes or legal results; suggest contacting the nursery or GreenRoot support.
- If asked about something unrelated to GreenRoot or nursery work, say briefly that you help with GreenRoot only.

About GreenRoot:
- Tabs: Home, Buying, Selling, Driver, Profile. The mango button also offers step-by-step guides on screen.
- Buying: ask a nursery for prices (Buying, Request prices), receive a quotation, accept it; the nursery turns it into an order.
- Selling (nursery owner or manager): create and send quotations; orders go PENDING, CONFIRMED, LOADING, LOADED, then a trip, delivered, COMPLETED.
- Loading: the nursery enters loaded quantities, adds photos and confirms the load.
- Trips: the nursery creates a trip and shares its QR code; an approved driver scans it and accepts with an approved lorry. At delivery the buyer tells the driver the 4-digit delivery code.
- Owners invite managers (gumasthas) and customers from Selling, People.
- Local Market: post or browse plant ads.
- Drivers: accept a trip by scanning its QR code (Driver tab, Scan); the trip screen shows where to deliver; start the trip, and at delivery enter the buyer's 4-digit code. A driver asking where to go: use list_trips with side driving (it gives deliver_to).`

// changeRule is per user: with one general rule the model still offered
// buyers to "prepare" a confirmation it had no tool for (seen 2026-10-30).
func changeRule(specs []toolSpec) string {
	if slices.ContainsFunc(specs, func(t toolSpec) bool { return t.Name == "propose_confirm_order" }) {
		return "You change nothing yourself. The only change you can prepare is confirming a pending order the user's nursery received (propose_confirm_order); then tell the user to tap Confirm below, and never say it is done. A Confirm button exists ONLY when propose_confirm_order returned awaiting_user_confirmation in this turn; otherwise never mention one. For any other change (create, send, cancel, edit, payments, trips) tell the user which screen to use."
	}
	return "You cannot change anything for this user and there is no Confirm button. If they ask to confirm, cancel or change something, never say you will do or prepare it: say who can (the selling nursery confirms orders) or which screen to use."
}

type turn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// converse answers one message. Tools run as the user; the model sees only
// what they return.
func (h *Handler) converse(ctx context.Context, env *toolEnv, requestID string, history []turn, message string) (string, []string, error) {
	roles := strings.Join(env.actor.Roles, ", ")
	if roles == "" {
		roles = "BUYER"
	}
	// Decided here, not left to the model: told that users speak Telugu, it
	// answered English questions in Telugu (seen 2026-10-30).
	language := "English, because the user wrote in English"
	if hasTelugu(message) {
		language = "Telugu, in Telugu script (English words such as order or status may stay in English)"
	}
	specs := toolsFor(env)
	msgs := []chatMessage{{Role: "system", Content: fmt.Sprintf(systemPrompt, env.now.In(env.loc).Format("Monday 2 January 2006"), roles, language, changeRule(specs))}}
	for _, t := range history {
		msgs = append(msgs, chatMessage{Role: t.Role, Content: t.Text})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: message})
	var used []string
	for round := 0; ; round++ {
		offer := specs
		if round == maxToolRounds {
			offer = nil
		}
		reply, err := h.ai.chat(ctx, requestID, msgs, offer)
		if err != nil {
			return "", used, err
		}
		h.ledger.record(ctx, h.log, env.actor.UserID, kindLLM, reply.Usage, requestID)
		if len(reply.ToolCalls) == 0 || offer == nil {
			return strings.TrimSpace(reply.Content), used, nil
		}
		msgs = append(msgs, chatMessage{Role: "assistant", Content: reply.Content, ToolCalls: reply.ToolCalls})
		// Every call needs an answer, or the provider rejects the next request.
		for i, call := range reply.ToolCalls {
			var result any = toolError("too_many", "ask fewer things at once")
			if i < 4 {
				result = runTool(ctx, env, call.Name, call.Arguments)
				used = append(used, call.Name)
			}
			raw, _ := json.Marshal(result)
			msgs = append(msgs, chatMessage{Role: "tool", ToolCallID: call.ID, Content: string(raw)})
		}
	}
}
