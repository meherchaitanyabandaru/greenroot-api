package apitest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	baseURL = "http://localhost:8080"

	adminPhone   = "9000000000"
	ownerPhone   = "9100000000"
	managerPhone = "9200000000"
	buyerPhone   = "9300000000"
	driverPhone  = "9400000000"
	devOTP       = "123456"
)

func TestMain(m *testing.M) {
	if err := seedAPITestFixtures(); err != nil {
		fmt.Fprintf(os.Stderr, "seed API test fixtures: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func seedAPITestFixtures() error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres:///greenroot?host=/tmp"
	}
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return err
	}

	const fixtureSQL = `
INSERT INTO public.users
  (user_id, user_code, first_name, last_name, mobile, email, mobile_verified, email_verified, gender, status, created_at, updated_at)
VALUES
  (2, 'USR-000002', 'Test', 'Owner', '9100000000', 'owner@greenroot.test', true, true, 'MALE', 'ACTIVE', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (3, 'USR-000003', 'Test', 'Manager', '9200000000', 'manager@greenroot.test', true, true, 'MALE', 'ACTIVE', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (4, 'USR-000004', 'Test', 'Buyer', '9300000000', 'buyer@greenroot.test', true, true, 'FEMALE', 'ACTIVE', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (5, 'USR-000005', 'Test', 'Driver', '9400000000', 'driver@greenroot.test', true, true, 'MALE', 'ACTIVE', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (user_id) DO UPDATE SET
  first_name = EXCLUDED.first_name,
  last_name = EXCLUDED.last_name,
  mobile = EXCLUDED.mobile,
  email = EXCLUDED.email,
  mobile_verified = true,
  email_verified = true,
  gender = EXCLUDED.gender,
  status = 'ACTIVE',
  deleted_at = NULL,
  updated_at = CURRENT_TIMESTAMP;

DELETE FROM public.user_roles WHERE user_id IN (2, 3, 4, 5);

INSERT INTO public.user_roles (user_id, role_id)
SELECT u.user_id, r.role_id
FROM (VALUES
  (2, 'BUYER'), (2, 'NURSERY_OWNER'),
  (3, 'MANAGER'),
  (4, 'BUYER'),
  (5, 'DRIVER')
) AS want(user_id, role_code)
JOIN public.roles r ON r.role_code = want.role_code
JOIN public.users u ON u.user_id = want.user_id
WHERE true
ON CONFLICT DO NOTHING;

INSERT INTO public.nurseries
  (nursery_id, nursery_code, nursery_name, owner_user_id, mobile, email, website, description, status, approved_at, created_by, created_at, updated_at)
VALUES
  (1, 'NUR-000001', 'API Test Nursery', 2, '9100000000', 'nursery@greenroot.test', 'https://greenroot.test', 'API fixture nursery', 'APPROVED', CURRENT_TIMESTAMP, 2, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (nursery_id) DO UPDATE SET
  nursery_name = EXCLUDED.nursery_name,
  owner_user_id = EXCLUDED.owner_user_id,
  mobile = EXCLUDED.mobile,
  email = EXCLUDED.email,
  status = 'APPROVED',
  approved_at = COALESCE(public.nurseries.approved_at, CURRENT_TIMESTAMP),
  deleted_at = NULL,
  updated_at = CURRENT_TIMESTAMP;

DELETE FROM public.nursery_users WHERE user_id = 3;

INSERT INTO public.nursery_users (nursery_id, user_id, role, status, nursery_role_id, invited_by_user_id, is_active, joined_at, updated_at)
SELECT 1, 3, 'MANAGER', 'ACTIVE', nr.nursery_role_id, 2, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM public.nursery_roles nr
WHERE nr.role_code = 'MANAGER';

INSERT INTO public.subscription_plans
  (plan_code, plan_name, description, monthly_price, yearly_price, max_managers, is_active, features)
VALUES
  ('FREE', 'Free', 'Free starter plan for public buying and basic GreenRoot access.', 0, 0, 0, true, '{"billing_cycles":["FREE"]}'::jsonb),
  ('TRIAL', 'Community Trial', '6-month free trial for new approved nurseries.', 0, 0, 1, true, '{"billing_cycles":["TRIAL"]}'::jsonb)
ON CONFLICT (plan_code) DO UPDATE SET
  plan_name = EXCLUDED.plan_name,
  description = EXCLUDED.description,
  monthly_price = EXCLUDED.monthly_price,
  yearly_price = EXCLUDED.yearly_price,
  max_managers = EXCLUDED.max_managers,
  is_active = true,
  features = public.subscription_plans.features || EXCLUDED.features,
  updated_at = CURRENT_TIMESTAMP;

DELETE FROM public.user_subscriptions WHERE user_id = 2;

INSERT INTO public.user_subscriptions
  (subscription_code, user_id, plan_id, start_date, end_date, subscription_status, auto_renew, created_at, updated_at)
SELECT 'SUB-API-OWNER', 2, plan_id, CURRENT_DATE, CURRENT_DATE + INTERVAL '180 days', 'TRIAL', false, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM public.subscription_plans
WHERE plan_code = 'TRIAL'
AND true
ON CONFLICT (subscription_code) DO UPDATE SET
  user_id = EXCLUDED.user_id,
  plan_id = EXCLUDED.plan_id,
  start_date = EXCLUDED.start_date,
  end_date = EXCLUDED.end_date,
  subscription_status = 'TRIAL',
  updated_at = CURRENT_TIMESTAMP;

SELECT setval('public.users_user_id_seq', GREATEST((SELECT COALESCE(MAX(user_id), 1) FROM public.users), 1), true);
SELECT setval('public.nurseries_nursery_id_seq', GREATEST((SELECT COALESCE(MAX(nursery_id), 1) FROM public.nurseries), 1), true);
SELECT setval('public.nursery_users_nursery_user_id_seq', GREATEST((SELECT COALESCE(MAX(nursery_user_id), 1) FROM public.nursery_users), 1), true);
SELECT setval('public.subscription_plans_plan_id_seq', GREATEST((SELECT COALESCE(MAX(plan_id), 1) FROM public.subscription_plans), 1), true);
SELECT setval('public.user_subscriptions_user_subscription_id_seq', GREATEST((SELECT COALESCE(MAX(user_subscription_id), 1) FROM public.user_subscriptions), 1), true);

INSERT INTO public.public_code_sequences (code_key, date_key, last_value) VALUES
  ('users', '', GREATEST((SELECT COALESCE(MAX((regexp_replace(user_code, '[^0-9]', '', 'g'))::integer), 0) FROM public.users), 5)),
  ('nurseries', '', GREATEST((SELECT COALESCE(MAX((regexp_replace(nursery_code, '[^0-9]', '', 'g'))::integer), 0) FROM public.nurseries), 1)),
  ('user_subscriptions', '', GREATEST((SELECT COALESCE(MAX((regexp_replace(subscription_code, '[^0-9]', '', 'g'))::integer), 0) FROM public.user_subscriptions WHERE subscription_code ~ '[0-9]'), 1))
ON CONFLICT (code_key, date_key) DO UPDATE SET
  last_value = GREATEST(public.public_code_sequences.last_value, EXCLUDED.last_value),
  updated_at = CURRENT_TIMESTAMP;
`
	_, err = db.ExecContext(ctx, fixtureSQL)
	return err
}

type testWorkspace struct {
	Type      string `json:"type"`
	NurseryID int64  `json:"nursery_id"`
}

func getWorkspaces(t *testing.T, token string) []testWorkspace {
	t.Helper()
	resp := get(t, "/api/v1/me/workspaces", token)
	assertStatus(t, resp, http.StatusOK)

	var workspaces []testWorkspace
	decode(t, resp, &workspaces)
	return workspaces
}

func login(t *testing.T, mobile string) string {
	t.Helper()

	sendOTP(t, mobile)

	body := map[string]string{"mobile": mobile, "otp": devOTP}
	resp := post(t, "/api/v1/auth/verify-otp", body, "")

	var result struct {
		AccessToken string `json:"access_token"`
	}
	decode(t, resp, &result)

	if result.AccessToken == "" {
		t.Fatalf("login(%s): empty access_token", mobile)
	}
	return result.AccessToken
}

func sendOTP(t *testing.T, mobile string) {
	t.Helper()
	body := map[string]string{"mobile": mobile}
	resp := post(t, "/api/v1/auth/send-otp", body, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("send-otp(%s): got %d", mobile, resp.StatusCode)
	}
}

func get(t *testing.T, path, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func post(t *testing.T, path string, body any, token string) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, b)
	}
}

func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("status: got %d, want %d\nbody: %s", resp.StatusCode, want, b)
	}
}

func putReq(t *testing.T, path string, body any, token string) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, baseURL+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	return resp
}

func deleteReq(t *testing.T, path, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, baseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	return resp
}

func url(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
