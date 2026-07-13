#!/usr/bin/env sh
set -eu

DB_NAME="${INTEGRATION_DB_NAME:-greenroot_integration_$$}"
PORT="${INTEGRATION_PORT:-18096}"
BASE_URL="http://127.0.0.1:$PORT"
TEST_LOG_DIR="${TEST_LOG_DIR:-test-log}"
INTEGRATION_TEST_LOG_DIR="$TEST_LOG_DIR/integration"
LOG_DIR="${INTEGRATION_LOG_DIR:-$INTEGRATION_TEST_LOG_DIR/runtime}"
API_PROCESS_LOG="$INTEGRATION_TEST_LOG_DIR/api-process.log"
RESULT_LOG="$INTEGRATION_TEST_LOG_DIR/results.log"
JWT_SECRET="${JWT_SECRET:-integration-secret}"
RESET_SQL="${RESET_SQL:-../greenroot-infra/db/postgresql/reset-local.sql}"
DATABASE_URL="postgres:///$DB_NAME?host=/tmp"

cleanup() {
  if [ "${SERVER_PID:-}" ]; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
  dropdb "$DB_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

mkdir -p "$INTEGRATION_TEST_LOG_DIR" "$LOG_DIR"
: > "$API_PROCESS_LOG"
: > "$RESULT_LOG"

createdb "$DB_NAME"
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$RESET_SQL" >/dev/null

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
WITH upserted AS (
  INSERT INTO public.users (first_name, last_name, mobile, email, mobile_verified, email_verified, status, created_at, updated_at, gender)
  VALUES
    ('Integration', 'Admin', '9100000001', 'integration.admin@greenroot.test', true, false, 'ACTIVE', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'PREFER_NOT_TO_SAY'),
    ('Integration', 'Buyer', '9100000002', 'integration.buyer@greenroot.test', true, false, 'ACTIVE', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'PREFER_NOT_TO_SAY'),
    ('Integration', 'Nursery', '9100000003', 'integration.nursery@greenroot.test', true, false, 'ACTIVE', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'PREFER_NOT_TO_SAY'),
    ('Integration', 'Driver', '9100000004', 'integration.driver@greenroot.test', true, false, 'ACTIVE', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'PREFER_NOT_TO_SAY')
  ON CONFLICT (mobile) DO UPDATE
    SET first_name = EXCLUDED.first_name,
        last_name = EXCLUDED.last_name,
        email = EXCLUDED.email,
        status = 'ACTIVE',
        updated_at = CURRENT_TIMESTAMP
  RETURNING user_id, mobile
)
INSERT INTO public.user_roles (user_id, role_id, assigned_at)
SELECT u.user_id, r.role_id, CURRENT_TIMESTAMP
FROM upserted u
JOIN public.roles r ON (
  (u.mobile = '9100000001' AND r.role_code = 'ADMIN') OR
  (u.mobile = '9100000002' AND r.role_code = 'BUYER') OR
  (u.mobile = '9100000003' AND r.role_code = 'NURSERY_OWNER') OR
  (u.mobile = '9100000004' AND r.role_code = 'DRIVER')
)
ON CONFLICT DO NOTHING;

INSERT INTO public.nurseries
  (nursery_id, nursery_code, nursery_name, owner_user_id, mobile, status, created_at, updated_at)
SELECT 1, 'NUR-INTEGRATION-001', 'Integration Nursery', u.user_id, '9100000003', 'APPROVED', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM public.users u
WHERE u.mobile = '9100000003'
ON CONFLICT (nursery_id) DO UPDATE
  SET nursery_name = EXCLUDED.nursery_name,
      owner_user_id = EXCLUDED.owner_user_id,
      status = 'APPROVED',
      updated_at = CURRENT_TIMESTAMP;

INSERT INTO public.nursery_users (nursery_id, user_id, nursery_role_id, joined_at, is_active)
SELECT 1, u.user_id, COALESCE(nr.nursery_role_id, 1), CURRENT_TIMESTAMP, true
FROM public.users u
LEFT JOIN public.nursery_roles nr ON nr.role_code IN ('OWNER', 'ADMIN', 'MANAGER')
WHERE u.mobile = '9100000003'
ON CONFLICT DO NOTHING;

SELECT setval('public.nurseries_nursery_id_seq', (SELECT max(nursery_id) FROM public.nurseries), true);
SQL

DATABASE_URL="$DATABASE_URL" HTTP_PORT="$PORT" LOG_DIR="$LOG_DIR" JWT_SECRET="$JWT_SECRET" go run ./cmd/api >"$API_PROCESS_LOG" 2>&1 &
SERVER_PID="$!"

for _ in $(seq 1 50); do
  if curl -fsS "$BASE_URL/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done

if INTEGRATION_BASE_URL="$BASE_URL" go run ./cmd/integration >"$RESULT_LOG" 2>&1; then
  cat "$RESULT_LOG"
else
  cat "$RESULT_LOG"
  echo "integration API process log: $API_PROCESS_LOG" >&2
  echo "integration runtime logs: $LOG_DIR" >&2
  exit 1
fi
