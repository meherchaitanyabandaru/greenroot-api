package apitest

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Mango is for owners, managers and customers; drivers and admins are refused.
func TestAssistant_OnlyOwnersManagersAndCustomers(t *testing.T) {
	for _, c := range []struct {
		mobile string
		status int
		code   string
	}{
		{ownerPhone, 200, ""},
		{managerPhone, 200, ""},
		{buyerPhone, 200, ""},
		{driverPhone, 403, "assistant_role_not_allowed"},
		{adminPhone, 403, "assistant_not_for_admins"},
	} {
		resp := get(t, "/api/v1/assistant/status", login(t, c.mobile))
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		code := resp.StatusCode
		decode(t, resp, &body)
		if code != c.status || body.Error.Code != c.code {
			t.Errorf("%s: %d %q, want %d %q", c.mobile, code, body.Error.Code, c.status, c.code)
		}
	}
	// Drivers are refused before anything else, AI on or off.
	resp := mango(t, http.MethodPost, "/api/v1/assistant/chat", `{"message":"hi"}`, login(t, driverPhone))
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("driver chat: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func mango(t *testing.T, method, path, body, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, baseURL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Mango-Notice", "1")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

type mangoStatus struct {
	Enabled   bool `json:"enabled"`
	Questions struct {
		Used     int       `json:"questions_used"`
		Limit    int       `json:"questions_limit"`
		Left     int       `json:"questions_left"`
		ResetsAt time.Time `json:"resets_at"`
	} `json:"questions"`
}

// The count is per India-time day: yesterday's questions do not count today;
// a used-up day refuses new questions before any AI call; deleting Mango data
// does not reset it.
func TestAssistant_DailyQuestionsResetAndRefuseBeforeAI(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres:///greenroot?host=/tmp"
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() }) // after the row cleanup below (cleanups run last-first)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	const user = 3 // fixture buyer
	ist, _ := time.LoadLocation("Asia/Kolkata")
	today := time.Now().In(ist)
	clear := func() {
		if _, err := db.ExecContext(ctx, `DELETE FROM public.assistant_question_days WHERE user_id = $1`, user); err != nil {
			t.Fatal(err)
		}
	}
	clear()
	t.Cleanup(clear)
	token := login(t, buyerPhone)
	status := func() mangoStatus {
		var s mangoStatus
		decode(t, get(t, "/api/v1/assistant/status", token), &s)
		return s
	}

	limit := status().Questions.Limit
	if limit < 1 {
		t.Fatalf("daily limit %d", limit)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO public.assistant_question_days (user_id, question_date, questions_used) VALUES ($1, $2, $3)`,
		user, today.AddDate(0, 0, -1).Format("2006-01-02"), limit); err != nil {
		t.Fatal(err)
	}
	if s := status(); s.Questions.Used != 0 || s.Questions.Left != limit {
		t.Errorf("a new day starts fresh: %+v", s.Questions)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO public.assistant_question_days (user_id, question_date, questions_used) VALUES ($1, $2, $3)`,
		user, today.Format("2006-01-02"), limit); err != nil {
		t.Fatal(err)
	}
	s := status()
	y, m, d := today.Date()
	if s.Questions.Left != 0 || !s.Questions.ResetsAt.Equal(time.Date(y, m, d+1, 0, 0, 0, 0, ist)) {
		t.Errorf("used up today, resets at India midnight: %+v", s.Questions)
	}

	if !s.Enabled {
		t.Skip("the rest needs an API with AI_ENABLED=true (as in CI)")
	}
	for _, path := range []string{"/api/v1/assistant/chat", "/api/v1/assistant/transcribe"} {
		resp := mango(t, http.MethodPost, path, `{"message":"pending orders?"}`, token)
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		code := resp.StatusCode
		decode(t, resp, &body)
		if code != http.StatusTooManyRequests || body.Error.Code != "ai_daily_questions_used" {
			t.Errorf("%s with no questions left: %d %q", path, code, body.Error.Code)
		}
	}
	resp := deleteReq(t, "/api/v1/assistant/data", token)
	resp.Body.Close()
	if s := status(); s.Questions.Left != 0 {
		t.Errorf("deleting Mango data must not reset today's count: %+v", s.Questions)
	}
}
