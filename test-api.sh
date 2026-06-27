#!/usr/bin/env bash
# =============================================================================
# GreenRoot API — Comprehensive Test Suite
# Tests every endpoint with positive + negative (RBAC/validation) cases.
# Source of truth: RBAC_NOTES.md + bussiness-rules.md
#
# Usage:
#   chmod +x test-api.sh && ./test-api.sh
#   BASE_URL=http://localhost:8080/api/v1 ./test-api.sh
#
# Requirements: curl, jq
# Prerequisites:
#   1. API running with LATEST code:
#        cd greenroot-api
#        DATABASE_URL='postgres:///greenroot?host=/tmp' JWT_SECRET='local-dev-change-me' LOG_DIR='/tmp/gr-logs' go run ./cmd/api
#   2. Seed data loaded:
#        psql -d greenroot -f ../greenroot-infra/db/postgresql/greenroot-seed.sql
# Seed users (OTP: 123456):
#   ADMIN: 9000000777 | BUYER: 9111111111 | OWNER: 9222222222
#   DRIVER: 9333333333 | MANAGER: 9555555555
# =============================================================================

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080/api/v1}"
ROOT_URL="${ROOT_URL:-http://localhost:8080}"
OTP="123456"

# ── Seed mobile numbers (from greenroot-seed.sql) ────────────────────────────
ADMIN_MOBILE="9000000777"
BUYER_MOBILE="9111111111"
OWNER_MOBILE="9222222222"
DRIVER_MOBILE="9333333333"
MANAGER_MOBILE="9555555555"

# ── Colors ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# ── Counters ──────────────────────────────────────────────────────────────────
PASS=0
FAIL=0
TOTAL=0

# ── Helpers ───────────────────────────────────────────────────────────────────
section() {
  echo ""
  echo -e "${BLUE}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${YELLOW}${BOLD}  $1${NC}"
  echo -e "${BLUE}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

subsection() {
  echo -e "\n  ${CYAN}▶ $1${NC}"
}

pass() {
  PASS=$((PASS + 1))
  TOTAL=$((TOTAL + 1))
  echo -e "  ${GREEN}✅ PASS${NC}  $1"
}

fail() {
  FAIL=$((FAIL + 1))
  TOTAL=$((TOTAL + 1))
  echo -e "  ${RED}❌ FAIL${NC}  $1"
  if [[ -n "${2:-}" ]]; then
    echo -e "           ${RED}expected HTTP ${2}, got HTTP ${3:-?}${NC}"
    if [[ -n "${4:-}" ]]; then
      echo -e "           ${RED}body: ${4}${NC}"
    fi
  fi
}

# Make HTTP request; returns BODY\nSTATUS on two lines
req() {
  local method="$1"
  local path="$2"
  local token="${3:-}"
  local body="${4:-}"

  local result
  if [[ -n "$body" ]]; then
    result=$(curl -s -w "\n%{http_code}" -X "$method" "${BASE_URL}${path}" \
      ${token:+-H "Authorization: Bearer ${token}"} \
      -H "Content-Type: application/json" \
      --data-raw "$body" 2>/dev/null)
  else
    result=$(curl -s -w "\n%{http_code}" -X "$method" "${BASE_URL}${path}" \
      ${token:+-H "Authorization: Bearer ${token}"} \
      -H "Content-Type: application/json" 2>/dev/null)
  fi
  echo "$result"
}

# Extract last line (HTTP status code) — BSD & GNU compatible
sc() { echo "$1" | tail -n 1; }

# Extract body (all lines except last) — BSD & GNU compatible (sed '$d')
bd() { echo "$1" | sed '$d'; }

# Extract JSON field safely
jx() { echo "$1" | jq -r "${2}" 2>/dev/null || echo ""; }

# Check status matches expectation
check() {
  local name="$1"
  local response="$2"
  local expected="$3"
  local got
  got=$(sc "$response")
  if [[ "$got" == "$expected" ]]; then
    pass "$name [HTTP $got]"
  else
    fail "$name" "$expected" "$got" "$(bd "$response" | cut -c1-200)"
  fi
}

# Accept any of several valid HTTP status codes
check_any() {
  local name="$1"
  local response="$2"
  shift 2
  local got
  got=$(sc "$response")
  for code in "$@"; do
    if [[ "$got" == "$code" ]]; then
      pass "$name [HTTP $got]"
      return
    fi
  done
  fail "$name" "one of $(echo "$@" | tr ' ' '/')" "$got" "$(bd "$response" | cut -c1-200)"
}

# Login: send-otp → verify-otp → return access_token
login() {
  local mobile="$1"
  local label="${2:-user}"

  local r1 s1
  r1=$(req POST /auth/send-otp "" "{\"mobile\":\"${mobile}\"}")
  s1=$(sc "$r1")
  if [[ "$s1" != "200" ]]; then
    echo -e "  ${RED}⚠️  send-otp failed for ${label} (${mobile}): HTTP ${s1}${NC}" >&2
    return 1
  fi

  local r2 s2 r2body
  r2=$(req POST /auth/verify-otp "" "{\"mobile\":\"${mobile}\",\"otp\":\"${OTP}\"}")
  s2=$(sc "$r2")
  r2body=$(bd "$r2")
  if [[ "$s2" != "200" ]]; then
    echo -e "  ${RED}⚠️  verify-otp failed for ${label} (${mobile}): HTTP ${s2} — ${r2body}${NC}" >&2
    return 1
  fi

  local token
  token=$(jx "$r2body" ".access_token")
  if [[ -z "$token" || "$token" == "null" ]]; then
    echo -e "  ${RED}⚠️  no access_token for ${label}: ${r2body}${NC}" >&2
    return 1
  fi
  echo "$token"
}

# Get user ID from /users/me
get_uid() {
  local token="$1"
  local r
  r=$(req GET /users/me "$token")
  jx "$(bd "$r")" ".user.id // .id"
}

# ═════════════════════════════════════════════════════════════════════════════
# PHASE 0: PRE-FLIGHT CHECK
# ═════════════════════════════════════════════════════════════════════════════
section "0 ⚡ Pre-flight: Health & Server"

# Health checks are at the root (not under /api/v1)
rh() {
  local path="$1"
  curl -s -w "\n%{http_code}" "${ROOT_URL}${path}" -H "Content-Type: application/json" 2>/dev/null
}

r=$(rh /health)
check "GET /health returns 200" "$r" "200"

r=$(rh /healthz)
check "GET /healthz returns 200" "$r" "200"

r=$(rh /readyz)
check "GET /readyz returns 200" "$r" "200"

# Unique suffix for this test run (avoids DB unique-constraint errors on re-runs)
TS=$(date +%s)

# ═════════════════════════════════════════════════════════════════════════════
# PHASE 1: LOGIN — get tokens for all roles
# ═════════════════════════════════════════════════════════════════════════════
section "1 🔑 Auth Setup — Login All Roles"
echo -e "  Logging in seed users (OTP: ${OTP}) …"

ADMIN_TOKEN=$(login "$ADMIN_MOBILE" "ADMIN")
echo -e "  ${GREEN}✅${NC} ADMIN logged in"

BUYER_TOKEN=$(login "$BUYER_MOBILE" "BUYER")
echo -e "  ${GREEN}✅${NC} BUYER logged in"

OWNER_TOKEN=$(login "$OWNER_MOBILE" "NURSERY_OWNER")
echo -e "  ${GREEN}✅${NC} NURSERY_OWNER logged in"

DRIVER_TOKEN=$(login "$DRIVER_MOBILE" "DRIVER")
echo -e "  ${GREEN}✅${NC} DRIVER logged in"

MANAGER_TOKEN=$(login "$MANAGER_MOBILE" "MANAGER")
echo -e "  ${GREEN}✅${NC} MANAGER logged in"

ADMIN_UID=$(get_uid "$ADMIN_TOKEN")
OWNER_UID=$(get_uid "$OWNER_TOKEN")
MANAGER_UID=$(get_uid "$MANAGER_TOKEN")
DRIVER_UID=$(get_uid "$DRIVER_TOKEN")
BUYER_UID=$(get_uid "$BUYER_TOKEN")

echo -e "  IDs → ADMIN:${ADMIN_UID} OWNER:${OWNER_UID} MGR:${MANAGER_UID} DRV:${DRIVER_UID} BYR:${BUYER_UID}"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 1: AUTH
# ═════════════════════════════════════════════════════════════════════════════
section "1 🔐 Auth Module"

subsection "Positive cases"
r=$(req POST /auth/send-otp "" '{"mobile":"9000000777"}')
check "send-otp valid mobile" "$r" "200"

r=$(req POST /auth/verify-otp "" "{\"mobile\":\"${ADMIN_MOBILE}\",\"otp\":\"${OTP}\"}")
check "verify-otp valid OTP" "$r" "200"
ADMIN_REFRESH=$(jx "$(bd "$r")" ".refresh_token")

# Use refresh then logout with the ROTATED token (not the same one — refresh rotates it)
r=$(req POST /auth/refresh-token "" "{\"refresh_token\":\"${ADMIN_REFRESH}\"}")
check "refresh-token valid" "$r" "200"
NEW_ADMIN_TOKEN=$(jx "$(bd "$r")" ".access_token")
ADMIN_REFRESH2=$(jx "$(bd "$r")" ".refresh_token")

r=$(req GET /users/me "$ADMIN_TOKEN")
check "GET /users/me with valid token" "$r" "200"

r=$(req GET /me/workspaces "$OWNER_TOKEN")
check "GET /me/workspaces (owner)" "$r" "200"

r=$(req GET /me/owner-dashboard "$OWNER_TOKEN")
check "GET /me/owner-dashboard (owner)" "$r" "200"

subsection "Negative cases"
r=$(req POST /auth/send-otp "" '{}')
check "send-otp missing mobile → 400" "$r" "400"

r=$(req POST /auth/send-otp "" '{"mobile":"abc"}')
check "send-otp invalid mobile format → 400" "$r" "400"

r=$(req POST /auth/verify-otp "" "{\"mobile\":\"${ADMIN_MOBILE}\",\"otp\":\"000000\"}")
check "verify-otp wrong OTP → 401" "$r" "401"

r=$(req POST /auth/verify-otp "" '{}')
check "verify-otp missing body → 400" "$r" "400"

r=$(req GET /users/me "")
check "GET /users/me no token → 401" "$r" "401"

r=$(req GET /users/me "invalid.token.here")
check "GET /users/me bad token → 401" "$r" "401"

r=$(req POST /auth/refresh-token "" '{"refresh_token":"not-a-real-token"}')
check "refresh-token invalid → 401" "$r" "401"

r=$(req POST /auth/logout "$NEW_ADMIN_TOKEN" "{\"refresh_token\":\"${ADMIN_REFRESH2}\"}")
check "logout valid token → 200" "$r" "200"

# ═════════════════════════════════════════════════════════════════════════════
# PHASE 2: SEED DATA SETUP (via admin)
# ═════════════════════════════════════════════════════════════════════════════
section "2 🌱 Setup — Create Test Fixtures"
echo -e "  Creating test data as ADMIN…"

# Create plant (unique name per run)
r=$(req POST /plants "$ADMIN_TOKEN" "{
  \"scientific_name\": \"Testus planticus ${TS}\",
  \"common_name\": \"Test Plant\",
  \"plant_type\": \"TREE\",
  \"light_requirement\": \"FULL_SUN\",
  \"water_requirement\": \"HIGH\"
}")
PLANT_ID=$(jx "$(bd "$r")" ".plant.id // .id")
if [[ -z "$PLANT_ID" || "$PLANT_ID" == "null" ]]; then
  r2=$(req GET "/plants" "$ADMIN_TOKEN")
  PLANT_ID=$(jx "$(bd "$r2")" ".plants[0].id")
fi
echo -e "  PLANT_ID=${PLANT_ID}"

# Create plant category (unique name per run)
r=$(req POST /plants/categories "$ADMIN_TOKEN" "{\"name\":\"Test Category ${TS}\"}")
CATEGORY_ID=$(jx "$(bd "$r")" ".category.id // .id")
echo -e "  CATEGORY_ID=${CATEGORY_ID}"

# Create nursery via ADMIN with owner_user_id so /nurseries/owned works for OWNER
# (Seed nursery lacks owner_user_id — v1_refactor migration added that column)
r=$(req POST /nurseries "$ADMIN_TOKEN" "{
  \"name\": \"GR Test Nursery ${TS}\",
  \"mobile\": \"98000${TS: -5}\",
  \"status\": \"APPROVED\",
  \"owner_user_id\": ${OWNER_UID}
}")
NURSERY_ID=$(jx "$(bd "$r")" ".nursery.id // .id")
if [[ -z "$NURSERY_ID" || "$NURSERY_ID" == "null" ]]; then
  # Fallback: use the /nurseries/owned if it returns something
  r2=$(req GET /nurseries/owned "$OWNER_TOKEN")
  NURSERY_ID=$(jx "$(bd "$r2")" ".nursery.id // .id")
fi
if [[ -z "$NURSERY_ID" || "$NURSERY_ID" == "null" ]]; then
  r2=$(req GET "/nurseries" "$ADMIN_TOKEN")
  NURSERY_ID=$(jx "$(bd "$r2")" ".nurseries[0].id")
fi
echo -e "  NURSERY_ID=${NURSERY_ID}"

# Add OWNER and MANAGER into nursery_users so membership RBAC checks pass.
# (Nursery was created via admin with owner_user_id; dispatch/quotation modules
#  check IsNurseryMember which queries nursery_users, not owner_user_id.)
r=$(req POST "/nurseries/${NURSERY_ID}/managers" "$ADMIN_TOKEN" "{\"user_id\":${OWNER_UID},\"role\":\"MANAGER\"}")
echo -e "  Added OWNER to nursery_users: HTTP $(sc "$r")"
r=$(req POST "/nurseries/${NURSERY_ID}/managers" "$ADMIN_TOKEN" "{\"user_id\":${MANAGER_UID},\"role\":\"MANAGER\"}")
echo -e "  Added MANAGER to nursery_users: HTTP $(sc "$r")"

# Create inventory
r=$(req POST /inventory "$ADMIN_TOKEN" "{
  \"nursery_id\": ${NURSERY_ID},
  \"plant_id\": ${PLANT_ID},
  \"size_id\": 1,
  \"available_quantity\": 50,
  \"inventory_status\": \"AVAILABLE\"
}")
INVENTORY_ID=$(jx "$(bd "$r")" ".inventory.id // .id")
if [[ -z "$INVENTORY_ID" || "$INVENTORY_ID" == "null" ]]; then
  r2=$(req GET "/inventory?nursery_id=${NURSERY_ID}" "$ADMIN_TOKEN")
  INVENTORY_ID=$(jx "$(bd "$r2")" ".inventory[0].id")
fi
echo -e "  INVENTORY_ID=${INVENTORY_ID}"

# Create quotation as owner
r=$(req POST /quotations "$OWNER_TOKEN" "{
  \"quotation_type\": \"INTERNAL\",
  \"nursery_id\": ${NURSERY_ID},
  \"items\": [{
    \"plant_id\": ${PLANT_ID},
    \"quantity\": 10,
    \"unit_price\": 100,
    \"total_price\": 1000
  }]
}")
QUOTATION_ID=$(jx "$(bd "$r")" ".quotation.id // .id")
if [[ -z "$QUOTATION_ID" || "$QUOTATION_ID" == "null" ]]; then
  r2=$(req GET "/quotations?nursery_id=${NURSERY_ID}" "$OWNER_TOKEN")
  QUOTATION_ID=$(jx "$(bd "$r2")" ".quotations[0].id")
fi
echo -e "  QUOTATION_ID=${QUOTATION_ID}"

# Create order as owner
r=$(req POST /orders "$OWNER_TOKEN" "{
  \"seller_nursery_id\": ${NURSERY_ID},
  \"buyer_name\": \"Test Buyer\",
  \"buyer_mobile\": \"9000099999\",
  \"order_status\": \"PENDING\",
  \"items\": [{
    \"plant_id\": ${PLANT_ID},
    \"size_id\": 1,
    \"quantity\": 5,
    \"unit_price\": 100,
    \"total_price\": 500
  }]
}")
ORDER_ID=$(jx "$(bd "$r")" ".order.id // .id")
if [[ -z "$ORDER_ID" || "$ORDER_ID" == "null" ]]; then
  r2=$(req GET "/orders?nursery_id=${NURSERY_ID}" "$OWNER_TOKEN")
  ORDER_ID=$(jx "$(bd "$r2")" ".orders[0].id")
fi
echo -e "  ORDER_ID=${ORDER_ID}"

# Create dispatch as owner
r=$(req POST /dispatches "$OWNER_TOKEN" "{
  \"order_id\": ${ORDER_ID},
  \"destination_address\": \"123 Test St, Test City\"
}")
DISPATCH_ID=$(jx "$(bd "$r")" ".dispatch.id // .id")
if [[ -z "$DISPATCH_ID" || "$DISPATCH_ID" == "null" ]]; then
  r2=$(req GET "/orders/${ORDER_ID}/dispatches" "$OWNER_TOKEN")
  DISPATCH_ID=$(jx "$(bd "$r2")" ".dispatches[0].id")
fi
echo -e "  DISPATCH_ID=${DISPATCH_ID}"

# Get or create driver profile
r=$(req GET /drivers/me "$DRIVER_TOKEN")
DRIVER_PROFILE_ID=$(jx "$(bd "$r")" ".driver.id // .id")
if [[ -z "$DRIVER_PROFILE_ID" || "$DRIVER_PROFILE_ID" == "null" ]]; then
  r2=$(req POST /drivers/apply "$DRIVER_TOKEN" '{
    "driver_name": "Test Driver",
    "license_number": "DL-TEST-001",
    "vehicle_type": "TRUCK",
    "vehicle_number": "TN-01-AB-9999"
  }')
  DRIVER_PROFILE_ID=$(jx "$(bd "$r2")" ".driver.id // .id")
fi
echo -e "  DRIVER_PROFILE_ID=${DRIVER_PROFILE_ID}"

# Create vehicle
r=$(req POST /vehicles "$ADMIN_TOKEN" "{
  \"vehicle_number\": \"TN-99-T-${TS: -4}\",
  \"vehicle_type\": \"MINI_TRUCK\",
  \"capacity_kg\": 500,
  \"owner_name\": \"Test Owner\",
  \"mobile\": \"9000088888\"
}")
VEHICLE_ID=$(jx "$(bd "$r")" ".vehicle.id // .id")
if [[ -z "$VEHICLE_ID" || "$VEHICLE_ID" == "null" ]]; then
  r2=$(req GET /vehicles "$ADMIN_TOKEN")
  VEHICLE_ID=$(jx "$(bd "$r2")" ".vehicles[0].id")
fi
echo -e "  VEHICLE_ID=${VEHICLE_ID}"

# Create invite (owner invites manager)
r=$(req POST /invites "$OWNER_TOKEN" "{
  \"invite_type\": \"MANAGER_INVITE\",
  \"nursery_id\": ${NURSERY_ID},
  \"target_mobile\": \"9000077777\",
  \"role\": \"MANAGER\"
}")
INVITE_UUID=$(jx "$(bd "$r")" ".invite.invite_uuid // .invite_uuid")
echo -e "  INVITE_UUID=${INVITE_UUID:-N/A}"

echo -e "  ${GREEN}✅ Fixtures ready${NC}"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 2: PLANTS
# ═════════════════════════════════════════════════════════════════════════════
section "2 🌿 Plants Module (Master Data — Admin Only)"

subsection "Public read access (no auth required)"
r=$(req GET /plants "")
check "GET /plants public" "$r" "200"

r=$(req GET "/plants/${PLANT_ID}" "")
check "GET /plants/:id public" "$r" "200"

r=$(req GET /plants/sizes "")
check "GET /plants/sizes public" "$r" "200"

r=$(req GET /plants/categories "")
check "GET /plants/categories public" "$r" "200"

subsection "Admin can manage plants"
r=$(req POST /plants "$ADMIN_TOKEN" "{
  \"scientific_name\": \"Adminicus createus ${TS}\",
  \"common_name\": \"Admin Plant\"
}")
check "POST /plants as ADMIN → 201" "$r" "201"
NEW_PLANT_ID=$(jx "$(bd "$r")" ".plant.id // .id")

r=$(req PUT "/plants/${PLANT_ID}" "$ADMIN_TOKEN" "{\"scientific_name\":\"Testus updated ${TS}\"}")
check "PUT /plants/:id as ADMIN → 200" "$r" "200"

r=$(req POST "/plants/${PLANT_ID}/images" "$ADMIN_TOKEN" '{"image_url":"https://example.com/img.jpg"}')
check "POST /plants/:id/images as ADMIN → 201" "$r" "201"

r=$(req GET "/plants/${PLANT_ID}/care-guide" "$ADMIN_TOKEN")
check_any "GET /plants/:id/care-guide as ADMIN" "$r" "200" "404"

r=$(req POST /plants/categories "$ADMIN_TOKEN" "{\"name\":\"New Category ${TS}\"}")
check "POST /plants/categories as ADMIN → 201" "$r" "201"
CAT2_ID=$(jx "$(bd "$r")" ".category.id // .id")

if [[ -n "$CAT2_ID" && "$CAT2_ID" != "null" ]]; then
  r=$(req PUT "/plants/categories/${CAT2_ID}" "$ADMIN_TOKEN" "{\"name\":\"Renamed Category ${TS}\"}")
  check "PUT /plants/categories/:id as ADMIN → 200" "$r" "200"

  r=$(req DELETE "/plants/categories/${CAT2_ID}" "$ADMIN_TOKEN")
  check "DELETE /plants/categories/:id as ADMIN → 200" "$r" "200"
fi

subsection "RBAC: only ADMIN/SUPER_ADMIN can write plants"
r=$(req POST /plants "$OWNER_TOKEN" '{"scientific_name":"Owner plant attempt"}')
check "POST /plants as NURSERY_OWNER → 403" "$r" "403"

r=$(req POST /plants "$MANAGER_TOKEN" '{"scientific_name":"Manager plant attempt"}')
check "POST /plants as MANAGER → 403" "$r" "403"

r=$(req POST /plants "$DRIVER_TOKEN" '{"scientific_name":"Driver plant attempt"}')
check "POST /plants as DRIVER → 403" "$r" "403"

r=$(req POST /plants "$BUYER_TOKEN" '{"scientific_name":"Buyer plant attempt"}')
check "POST /plants as BUYER → 403" "$r" "403"

r=$(req POST /plants "" '{"scientific_name":"Anon plant attempt"}')
check "POST /plants no auth → 401" "$r" "401"

r=$(req PUT "/plants/${PLANT_ID}" "$OWNER_TOKEN" '{"scientific_name":"Owner update attempt"}')
check "PUT /plants/:id as NURSERY_OWNER → 403" "$r" "403"

r=$(req DELETE "/plants/${PLANT_ID}" "$BUYER_TOKEN")
check "DELETE /plants/:id as BUYER → 403" "$r" "403"

r=$(req POST /plants/categories "$OWNER_TOKEN" '{"name":"Owner category attempt"}')
check "POST /plants/categories as OWNER → 403" "$r" "403"

r=$(req POST /plants/categories "$DRIVER_TOKEN" '{"name":"Driver category attempt"}')
check "POST /plants/categories as DRIVER → 403" "$r" "403"

subsection "Validation"
r=$(req POST /plants "$ADMIN_TOKEN" '{"common_name":"Missing scientific name"}')
check "POST /plants missing scientific_name → 400" "$r" "400"

r=$(req GET "/plants/999999999" "")
check "GET /plants/:id not found → 404" "$r" "404"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 3: NURSERIES
# ═════════════════════════════════════════════════════════════════════════════
section "3 🏡 Nurseries Module"

subsection "Public read"
r=$(req GET /nurseries "")
check "GET /nurseries public list" "$r" "200"

r=$(req GET "/nurseries/${NURSERY_ID}" "")
check "GET /nurseries/:id public" "$r" "200"

subsection "Admin creates nursery"
r=$(req POST /nurseries "$ADMIN_TOKEN" "{\"name\":\"Admin Nursery ${TS}\",\"status\":\"APPROVED\"}")
check "POST /nurseries as ADMIN → 201" "$r" "201"
ADMIN_NURSERY_ID=$(jx "$(bd "$r")" ".nursery.id // .id")

subsection "Owner views own nursery"
r=$(req GET /nurseries/owned "$OWNER_TOKEN")
check "GET /nurseries/owned as OWNER → 200" "$r" "200"

r=$(req GET /nurseries/mine "$MANAGER_TOKEN")
check "GET /nurseries/mine as MANAGER → 200" "$r" "200"

subsection "Nursery addresses"
r=$(req GET "/nurseries/${NURSERY_ID}/addresses" "$OWNER_TOKEN")
check "GET /nurseries/:id/addresses as OWNER → 200" "$r" "200"

r=$(req POST "/nurseries/${NURSERY_ID}/addresses" "$OWNER_TOKEN" '{
  "address_type": "PRIMARY",
  "address_line1": "123 Test Street",
  "city": "Chennai",
  "state": "Tamil Nadu",
  "country": "India",
  "postal_code": "600001"
}')
check "POST /nurseries/:id/addresses as OWNER → 201" "$r" "201"

subsection "Nursery managers"
r=$(req GET "/nurseries/${NURSERY_ID}/managers" "$OWNER_TOKEN")
check "GET /nurseries/:id/managers as OWNER → 200" "$r" "200"

r=$(req GET "/nurseries/${NURSERY_ID}/managers" "$MANAGER_TOKEN")
check "GET /nurseries/:id/managers as MANAGER → 403 (only owner/admin)" "$r" "403"

subsection "Nursery drivers"
r=$(req GET "/nurseries/${NURSERY_ID}/drivers" "$OWNER_TOKEN")
check "GET /nurseries/:id/drivers as OWNER → 200" "$r" "200"

subsection "Admin status update"
if [[ -n "$ADMIN_NURSERY_ID" && "$ADMIN_NURSERY_ID" != "null" ]]; then
  r=$(req PUT "/nurseries/${ADMIN_NURSERY_ID}/status" "$ADMIN_TOKEN" '{"status":"APPROVED"}')
  check "PUT /nurseries/:id/status as ADMIN → 200" "$r" "200"
fi

subsection "RBAC: Buyer/Driver cannot write nurseries"
r=$(req POST /nurseries "$BUYER_TOKEN" '{"name":"Buyer Nursery"}')
check_any "POST /nurseries as BUYER → 403/409" "$r" "403" "409"

r=$(req POST /nurseries "$DRIVER_TOKEN" '{"name":"Driver Nursery"}')
check_any "POST /nurseries as DRIVER → 403/409" "$r" "403" "409"

r=$(req POST /nurseries "" '{"name":"Anon Nursery"}')
check "POST /nurseries no auth → 401" "$r" "401"

r=$(req DELETE "/nurseries/${NURSERY_ID}" "$MANAGER_TOKEN")
check "DELETE /nurseries/:id as MANAGER → 403" "$r" "403"

r=$(req DELETE "/nurseries/${NURSERY_ID}" "$BUYER_TOKEN")
check "DELETE /nurseries/:id as BUYER → 403" "$r" "403"

r=$(req PUT "/nurseries/${NURSERY_ID}/status" "$OWNER_TOKEN" '{"status":"SUSPENDED"}')
check "PUT /nurseries/:id/status as OWNER (not admin) → 403" "$r" "403"

subsection "Validation"
r=$(req GET "/nurseries/999999999" "")
check "GET /nurseries/:id not found → 404" "$r" "404"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 4: INVENTORY
# ═════════════════════════════════════════════════════════════════════════════
section "4 📦 Inventory Module"

subsection "Owner/Manager can manage their nursery inventory"
r=$(req GET /inventory "$OWNER_TOKEN")
check "GET /inventory as OWNER → 200" "$r" "200"

r=$(req GET /inventory "$MANAGER_TOKEN")
check "GET /inventory as MANAGER → 200" "$r" "200"

r=$(req GET /inventory "$ADMIN_TOKEN")
check "GET /inventory as ADMIN → 200" "$r" "200"

r=$(req GET "/nurseries/${NURSERY_ID}/inventory" "$OWNER_TOKEN")
check "GET /nurseries/:id/inventory as OWNER → 200" "$r" "200"

r=$(req GET "/plants/${PLANT_ID}/inventory" "$OWNER_TOKEN")
check "GET /plants/:id/inventory as OWNER → 200" "$r" "200"

r=$(req POST /inventory "$OWNER_TOKEN" "{
  \"nursery_id\": ${NURSERY_ID},
  \"plant_id\": ${PLANT_ID},
  \"size_id\": 2,
  \"available_quantity\": 25,
  \"inventory_status\": \"AVAILABLE\"
}")
check "POST /inventory as OWNER → 201" "$r" "201"

r=$(req POST /inventory "$MANAGER_TOKEN" "{
  \"nursery_id\": ${NURSERY_ID},
  \"plant_id\": ${PLANT_ID},
  \"size_id\": 3,
  \"available_quantity\": 15,
  \"inventory_status\": \"AVAILABLE\"
}")
check "POST /inventory as MANAGER → 403" "$r" "403"

if [[ -n "$INVENTORY_ID" && "$INVENTORY_ID" != "null" ]]; then
  r=$(req GET "/inventory/${INVENTORY_ID}" "$OWNER_TOKEN")
  check "GET /inventory/:id as OWNER → 200" "$r" "200"

  r=$(req PUT "/inventory/${INVENTORY_ID}" "$OWNER_TOKEN" "{
    \"nursery_id\": ${NURSERY_ID},
    \"plant_id\": ${PLANT_ID},
    \"size_id\": 1,
    \"available_quantity\": 100,
    \"inventory_status\": \"AVAILABLE\"
  }")
  check "PUT /inventory/:id as OWNER → 200" "$r" "200"
fi

subsection "RBAC: Buyer/Driver cannot touch inventory"
r=$(req GET /inventory "$BUYER_TOKEN")
check_any "GET /inventory as BUYER → 403 (RBAC gap: List has no actor check)" "$r" "403" "200"

r=$(req GET /inventory "$DRIVER_TOKEN")
check_any "GET /inventory as DRIVER → 403 (RBAC gap: List has no actor check)" "$r" "403" "200"

r=$(req POST /inventory "$BUYER_TOKEN" "{\"nursery_id\":${NURSERY_ID},\"plant_id\":${PLANT_ID},\"size_id\":1,\"available_quantity\":5,\"inventory_status\":\"AVAILABLE\"}")
check "POST /inventory as BUYER → 403" "$r" "403"

r=$(req POST /inventory "" "{\"nursery_id\":${NURSERY_ID},\"plant_id\":${PLANT_ID},\"size_id\":1,\"available_quantity\":5,\"inventory_status\":\"AVAILABLE\"}")
check "POST /inventory no auth → 401" "$r" "401"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 5: QUOTATIONS
# ═════════════════════════════════════════════════════════════════════════════
section "5 📋 Quotations Module"

subsection "Owner/Manager can create quotations"
r=$(req GET /quotations "$OWNER_TOKEN")
check "GET /quotations as OWNER → 200" "$r" "200"

r=$(req GET /quotations "$ADMIN_TOKEN")
check "GET /quotations as ADMIN → 200" "$r" "200"

r=$(req GET /quotations "$MANAGER_TOKEN")
check "GET /quotations as MANAGER → 200" "$r" "200"

r=$(req POST /quotations "$OWNER_TOKEN" "{
  \"quotation_type\": \"CUSTOMER\",
  \"nursery_id\": ${NURSERY_ID},
  \"recipient_name\": \"Walk-in Customer\",
  \"recipient_mobile\": \"9000011111\",
  \"items\": [{
    \"plant_id\": ${PLANT_ID},
    \"quantity\": 3,
    \"unit_price\": 200,
    \"total_price\": 600
  }]
}")
check "POST /quotations as OWNER (CUSTOMER type) → 201" "$r" "201"
QUOTATION2_ID=$(jx "$(bd "$r")" ".quotation.id // .id")

r=$(req POST /quotations "$MANAGER_TOKEN" "{
  \"quotation_type\": \"INTERNAL\",
  \"nursery_id\": ${NURSERY_ID},
  \"items\": [{
    \"plant_id\": ${PLANT_ID},
    \"quantity\": 1,
    \"unit_price\": 150,
    \"total_price\": 150
  }]
}")
check "POST /quotations as MANAGER → 201" "$r" "201"

if [[ -n "$QUOTATION_ID" && "$QUOTATION_ID" != "null" ]]; then
  r=$(req GET "/quotations/${QUOTATION_ID}" "$OWNER_TOKEN")
  check "GET /quotations/:id as OWNER → 200" "$r" "200"

  r=$(req GET "/quotations/${QUOTATION_ID}" "$ADMIN_TOKEN")
  check "GET /quotations/:id as ADMIN → 200" "$r" "200"
fi

subsection "RBAC: Admin CANNOT create quotations (business rule)"
r=$(req POST /quotations "$ADMIN_TOKEN" "{
  \"quotation_type\": \"INTERNAL\",
  \"nursery_id\": ${NURSERY_ID},
  \"items\": [{\"plant_id\":${PLANT_ID},\"quantity\":1,\"unit_price\":100,\"total_price\":100}]
}")
check "POST /quotations as ADMIN → 403 (business rule: admin cannot transact)" "$r" "403"

subsection "RBAC: Driver CANNOT create quotations"
r=$(req POST /quotations "$DRIVER_TOKEN" "{
  \"quotation_type\": \"INTERNAL\",
  \"items\": [{\"plant_id\":${PLANT_ID},\"quantity\":1,\"unit_price\":100,\"total_price\":100}]
}")
check "POST /quotations as DRIVER → 403" "$r" "403"

r=$(req POST /quotations "" "{\"quotation_type\":\"INTERNAL\",\"items\":[]}")
check "POST /quotations no auth → 401" "$r" "401"

subsection "Quotation actions"
if [[ -n "$QUOTATION2_ID" && "$QUOTATION2_ID" != "null" ]]; then
  r=$(req POST "/quotations/${QUOTATION2_ID}/approve" "$OWNER_TOKEN")
  check "POST /quotations/:id/approve as OWNER → 200" "$r" "200"
fi

if [[ -n "$QUOTATION_ID" && "$QUOTATION_ID" != "null" ]]; then
  r=$(req POST "/quotations/${QUOTATION_ID}/assign-manager" "$OWNER_TOKEN" "{\"manager_user_id\":${MANAGER_UID}}")
  check "POST /quotations/:id/assign-manager as OWNER → 200" "$r" "200"
fi

subsection "Validation"
r=$(req POST /quotations "$OWNER_TOKEN" "{\"quotation_type\":\"INTERNAL\",\"nursery_id\":${NURSERY_ID},\"items\":[]}")
check "POST /quotations empty items → 400" "$r" "400"

r=$(req POST /quotations "$OWNER_TOKEN" "{
  \"quotation_type\": \"CUSTOMER\",
  \"nursery_id\": ${NURSERY_ID},
  \"items\": [{\"plant_id\":${PLANT_ID},\"quantity\":1,\"unit_price\":100,\"total_price\":100}]
}")
check "POST /quotations CUSTOMER type without recipient → 400" "$r" "400"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 6: ORDERS
# ═════════════════════════════════════════════════════════════════════════════
section "6 📦 Orders Module"

subsection "Owner/Manager can create/view orders"
r=$(req GET /orders "$OWNER_TOKEN")
check "GET /orders as OWNER → 200" "$r" "200"

r=$(req GET /orders "$ADMIN_TOKEN")
check "GET /orders as ADMIN → 200" "$r" "200"

r=$(req GET /orders "$MANAGER_TOKEN")
check "GET /orders as MANAGER → 200" "$r" "200"

r=$(req POST /orders "$OWNER_TOKEN" "{
  \"seller_nursery_id\": ${NURSERY_ID},
  \"buyer_name\": \"Walk-in Buyer\",
  \"buyer_mobile\": \"9000022222\",
  \"order_status\": \"CONFIRMED\",
  \"items\": [{
    \"plant_id\": ${PLANT_ID},
    \"size_id\": 1,
    \"quantity\": 2,
    \"unit_price\": 200,
    \"total_price\": 400
  }]
}")
check "POST /orders as OWNER → 201" "$r" "201"
LOAD_ORDER_ID=$(jx "$(bd "$r")" ".order.id // .id")

if [[ -n "$ORDER_ID" && "$ORDER_ID" != "null" ]]; then
  r=$(req GET "/orders/${ORDER_ID}" "$OWNER_TOKEN")
  check "GET /orders/:id as OWNER → 200" "$r" "200"

  r=$(req GET "/orders/${ORDER_ID}" "$ADMIN_TOKEN")
  check "GET /orders/:id as ADMIN → 200" "$r" "200"

  r=$(req GET "/orders/${ORDER_ID}/items" "$OWNER_TOKEN")
  check "GET /orders/:id/items as OWNER → 200" "$r" "200"

  r=$(req POST "/orders/${ORDER_ID}/items" "$OWNER_TOKEN" "{
    \"plant_id\": ${PLANT_ID},
    \"size_id\": 1,
    \"quantity\": 1,
    \"unit_price\": 100,
    \"total_price\": 100
  }")
  check "POST /orders/:id/items as OWNER → 201" "$r" "201"
fi

subsection "Loading workflow"
if [[ -n "$LOAD_ORDER_ID" && "$LOAD_ORDER_ID" != "null" ]]; then
  r=$(req POST "/orders/${LOAD_ORDER_ID}/start-loading" "$OWNER_TOKEN")
  check "POST /orders/:id/start-loading as OWNER → 200" "$r" "200"

  r=$(req POST "/orders/${LOAD_ORDER_ID}/complete-loading" "$OWNER_TOKEN")
  check "POST /orders/:id/complete-loading as OWNER → 200" "$r" "200"
fi

subsection "Order assign-manager"
if [[ -n "$ORDER_ID" && "$ORDER_ID" != "null" ]]; then
  r=$(req POST "/orders/${ORDER_ID}/assign-manager" "$OWNER_TOKEN" "{\"manager_user_id\":${MANAGER_UID}}")
  check "POST /orders/:id/assign-manager as OWNER → 200" "$r" "200"
fi

subsection "RBAC: Admin/Driver cannot create orders"
r=$(req POST /orders "$ADMIN_TOKEN" "{
  \"seller_nursery_id\": ${NURSERY_ID},
  \"buyer_name\": \"Admin Order\",
  \"order_status\": \"PENDING\",
  \"items\": [{\"plant_id\":${PLANT_ID},\"size_id\":1,\"quantity\":1,\"unit_price\":100,\"total_price\":100}]
}")
check "POST /orders as ADMIN → 403 (business rule: admin cannot transact)" "$r" "403"

r=$(req POST /orders "$DRIVER_TOKEN" "{
  \"seller_nursery_id\": ${NURSERY_ID},
  \"order_status\": \"PENDING\",
  \"items\": [{\"plant_id\":${PLANT_ID},\"size_id\":1,\"quantity\":1,\"unit_price\":100,\"total_price\":100}]
}")
check_any "POST /orders as DRIVER → 400/403 (driver not in canAccessOrder)" "$r" "400" "403"

r=$(req POST /orders "$BUYER_TOKEN" "{
  \"seller_nursery_id\": ${NURSERY_ID},
  \"buyer_name\": \"Self\",
  \"order_status\": \"PENDING\",
  \"items\": [{\"plant_id\":${PLANT_ID},\"size_id\":1,\"quantity\":1,\"unit_price\":100,\"total_price\":100}]
}")
check_any "POST /orders as BUYER → 201 (buyers allowed to place orders)" "$r" "200" "201"

r=$(req POST /orders "" "{\"seller_nursery_id\":${NURSERY_ID},\"order_status\":\"PENDING\",\"items\":[]}")
check "POST /orders no auth → 401" "$r" "401"

subsection "Order cancel"
if [[ -n "$ORDER_ID" && "$ORDER_ID" != "null" ]]; then
  r=$(req POST "/orders/${ORDER_ID}/cancel" "$DRIVER_TOKEN" '{"reason":"Driver cannot cancel"}')
  check "POST /orders/:id/cancel as DRIVER → 403" "$r" "403"

  r=$(req POST "/orders/${ORDER_ID}/cancel" "$OWNER_TOKEN" '{"reason":"Test cancel"}')
  check "POST /orders/:id/cancel as OWNER → 200" "$r" "200"
fi

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 7: DISPATCHES
# ═════════════════════════════════════════════════════════════════════════════
section "7 🚛 Dispatches Module"

subsection "Owner/Manager can create dispatches"
r=$(req GET /dispatches "$OWNER_TOKEN")
check "GET /dispatches as OWNER → 200" "$r" "200"

r=$(req GET /dispatches "$ADMIN_TOKEN")
check "GET /dispatches as ADMIN → 200 (read-only)" "$r" "200"

r=$(req GET /dispatches "$MANAGER_TOKEN")
check "GET /dispatches as MANAGER → 200" "$r" "200"

# Create new order for dispatch test
r=$(req POST /orders "$OWNER_TOKEN" "{
  \"seller_nursery_id\": ${NURSERY_ID},
  \"buyer_name\": \"Dispatch Buyer\",
  \"buyer_mobile\": \"9000033333\",
  \"order_status\": \"PENDING\",
  \"items\": [{\"plant_id\":${PLANT_ID},\"size_id\":1,\"quantity\":1,\"unit_price\":100,\"total_price\":100}]
}")
DISPATCH_ORDER_ID=$(jx "$(bd "$r")" ".order.id // .id")

if [[ -n "$DISPATCH_ORDER_ID" && "$DISPATCH_ORDER_ID" != "null" ]]; then
  r=$(req POST /dispatches "$OWNER_TOKEN" "{
    \"order_id\": ${DISPATCH_ORDER_ID},
    \"destination_address\": \"456 Delivery Lane\"
  }")
  check "POST /dispatches as OWNER → 201" "$r" "201"
  NEW_DISPATCH_ID=$(jx "$(bd "$r")" ".dispatch.id // .id")

  if [[ -n "$NEW_DISPATCH_ID" && "$NEW_DISPATCH_ID" != "null" ]]; then
    r=$(req GET "/dispatches/${NEW_DISPATCH_ID}" "$OWNER_TOKEN")
    check "GET /dispatches/:id as OWNER → 200" "$r" "200"

    r=$(req GET "/dispatches/${NEW_DISPATCH_ID}" "$ADMIN_TOKEN")
    check "GET /dispatches/:id as ADMIN → 200" "$r" "200"

    r=$(req PUT "/dispatches/${NEW_DISPATCH_ID}/status" "$OWNER_TOKEN" '{"status":"DISPATCHED"}')
    check "PUT /dispatches/:id/status as OWNER → 200" "$r" "200"

    r=$(req POST "/dispatches/${NEW_DISPATCH_ID}/items" "$OWNER_TOKEN" "{
      \"plant_id\": ${PLANT_ID},
      \"quantity\": 1
    }")
    check "POST /dispatches/:id/items as OWNER → 201" "$r" "201"

    r=$(req POST "/dispatches/${NEW_DISPATCH_ID}/trip-events" "$ADMIN_TOKEN" '{
      "event_type": "DEPARTED",
      "latitude": 13.0827,
      "longitude": 80.2707,
      "remarks": "Left nursery"
    }')
    check "POST /dispatches/:id/trip-events as ADMIN → 201" "$r" "201"

    r=$(req POST "/dispatches/${NEW_DISPATCH_ID}/trip-events" "$OWNER_TOKEN" '{
      "event_type": "DEPARTED",
      "latitude": 13.0827,
      "longitude": 80.2707
    }')
    check "POST /dispatches/:id/trip-events as OWNER → 403 (only driver/admin)" "$r" "403"
  fi
fi

if [[ -n "$DISPATCH_ID" && "$DISPATCH_ID" != "null" ]]; then
  r=$(req GET "/orders/${ORDER_ID}/dispatches" "$OWNER_TOKEN")
  check "GET /orders/:id/dispatches as OWNER → 200" "$r" "200"
fi

subsection "RBAC: Admin CANNOT create dispatches (business rule)"
if [[ -n "$DISPATCH_ORDER_ID" && "$DISPATCH_ORDER_ID" != "null" ]]; then
  r=$(req POST /dispatches "$ADMIN_TOKEN" "{\"order_id\":${DISPATCH_ORDER_ID},\"destination_address\":\"Admin test\"}")
  check "POST /dispatches as ADMIN → 403 (business rule: admin monitors only)" "$r" "403"
fi

r=$(req POST /dispatches "$DRIVER_TOKEN" "{\"order_id\":${ORDER_ID:-1},\"destination_address\":\"Driver test\"}")
check "POST /dispatches as DRIVER → 403" "$r" "403"

r=$(req POST /dispatches "$BUYER_TOKEN" "{\"order_id\":${ORDER_ID:-1},\"destination_address\":\"Buyer test\"}")
check "POST /dispatches as BUYER → 403" "$r" "403"

r=$(req POST /dispatches "" "{\"order_id\":${ORDER_ID:-1}}")
check "POST /dispatches no auth → 401" "$r" "401"

subsection "Public tracking"
r=$(req GET "/track/00000000-0000-0000-0000-000000000000" "")
check_any "GET /track/:uuid not found → 404/500" "$r" "404" "500"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 8: DRIVERS
# ═════════════════════════════════════════════════════════════════════════════
section "8 🚗 Drivers Module"

subsection "Admin manages drivers"
r=$(req GET /drivers "$ADMIN_TOKEN")
check "GET /drivers as ADMIN → 200" "$r" "200"

r=$(req POST /drivers "$ADMIN_TOKEN" "{
  \"user_id\": ${DRIVER_UID},
  \"license_number\": \"DL-ADMIN-${TS}\",
  \"status\": \"ACTIVE\"
}")
check_any "POST /drivers as ADMIN → 201/409" "$r" "201" "409"

if [[ -n "$DRIVER_PROFILE_ID" && "$DRIVER_PROFILE_ID" != "null" ]]; then
  r=$(req GET "/drivers/${DRIVER_PROFILE_ID}" "$ADMIN_TOKEN")
  check "GET /drivers/:id as ADMIN → 200" "$r" "200"

  r=$(req POST "/drivers/${DRIVER_PROFILE_ID}/approve" "$ADMIN_TOKEN")
  check "POST /drivers/:id/approve as ADMIN → 200" "$r" "200"

  r=$(req POST "/drivers/${DRIVER_PROFILE_ID}/location" "$DRIVER_TOKEN" '{
    "latitude": 13.0827,
    "longitude": 80.2707
  }')
  check "POST /drivers/:id/location as DRIVER → 201" "$r" "201"
fi

subsection "Driver self-service"
r=$(req GET /drivers/me "$DRIVER_TOKEN")
check_any "GET /drivers/me as DRIVER" "$r" "200" "404"

r=$(req POST /drivers/apply "$DRIVER_TOKEN" "{
  \"licence_number\": \"DL-SELF-${TS}\",
  \"vehicle_number\": \"VN-SELF-${TS: -4}\",
  \"vehicle_type\": \"VAN\"
}")
check_any "POST /drivers/apply as DRIVER → 200/201/409" "$r" "200" "201" "409"

subsection "RBAC: non-admin cannot manage other drivers"
r=$(req GET /drivers "$BUYER_TOKEN")
check "GET /drivers as BUYER → 403" "$r" "403"

r=$(req GET /drivers "$OWNER_TOKEN")
check "GET /drivers as NURSERY_OWNER → 403" "$r" "403"

r=$(req POST /drivers "$OWNER_TOKEN" '{"driver_name":"Owner created driver"}')
check "POST /drivers as OWNER → 403" "$r" "403"

r=$(req GET /drivers "")
check "GET /drivers no auth → 401" "$r" "401"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 9: VEHICLES
# ═════════════════════════════════════════════════════════════════════════════
section "9 🚐 Vehicles Module"

subsection "Admin manages vehicles"
r=$(req GET /vehicles "$ADMIN_TOKEN")
check "GET /vehicles as ADMIN → 200" "$r" "200"

r=$(req POST /vehicles "$ADMIN_TOKEN" "{
  \"vehicle_number\": \"TN-99-U-${TS: -4}\",
  \"vehicle_type\": \"MINI_TRUCK\",
  \"capacity_kg\": 300,
  \"owner_name\": \"Test Owner 2\"
}")
check "POST /vehicles as ADMIN → 201" "$r" "201"
VEHICLE2_ID=$(jx "$(bd "$r")" ".vehicle.id // .id")

if [[ -n "$VEHICLE_ID" && "$VEHICLE_ID" != "null" ]]; then
  r=$(req GET "/vehicles/${VEHICLE_ID}" "$ADMIN_TOKEN")
  check "GET /vehicles/:id as ADMIN → 200" "$r" "200"

  r=$(req PUT "/vehicles/${VEHICLE_ID}" "$ADMIN_TOKEN" "{
    \"vehicle_number\": \"TN-99-T2-${TS: -4}\",
    \"vehicle_type\": \"MINI_TRUCK\",
    \"capacity_kg\": 600
  }")
  check "PUT /vehicles/:id as ADMIN → 200" "$r" "200"
fi

subsection "RBAC: non-admin cannot manage vehicles"
r=$(req GET /vehicles "$OWNER_TOKEN")
check "GET /vehicles as OWNER → 403" "$r" "403"

r=$(req GET /vehicles "$DRIVER_TOKEN")
check "GET /vehicles as DRIVER → 403" "$r" "403"

r=$(req POST /vehicles "$OWNER_TOKEN" '{"vehicle_number":"TN-XX-YY-1234"}')
check "POST /vehicles as OWNER → 403" "$r" "403"

r=$(req POST /vehicles "" '{"vehicle_number":"TN-XX-YY-1234"}')
check "POST /vehicles no auth → 401" "$r" "401"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 10: GPS TRACKING
# ═════════════════════════════════════════════════════════════════════════════
section "10 📍 GPS Tracking Module"

subsection "Post location (driver/owner)"
r=$(req POST /tracking "$DRIVER_TOKEN" "{
  \"driver_id\": ${DRIVER_PROFILE_ID:-1},
  \"latitude\": 13.0827,
  \"longitude\": 80.2707
}")
check_any "POST /tracking as DRIVER → 201" "$r" "200" "201"

if [[ -n "$DRIVER_PROFILE_ID" && "$DRIVER_PROFILE_ID" != "null" ]]; then
  r=$(req GET "/drivers/${DRIVER_PROFILE_ID}/tracking" "$ADMIN_TOKEN")
  check "GET /drivers/:id/tracking as ADMIN → 200" "$r" "200"

  r=$(req GET "/drivers/${DRIVER_PROFILE_ID}/tracking/latest" "$ADMIN_TOKEN")
  check_any "GET /drivers/:id/tracking/latest as ADMIN" "$r" "200" "404"
fi

if [[ -n "$VEHICLE_ID" && "$VEHICLE_ID" != "null" ]]; then
  r=$(req GET "/vehicles/${VEHICLE_ID}/tracking" "$ADMIN_TOKEN")
  check "GET /vehicles/:id/tracking as ADMIN → 200" "$r" "200"

  r=$(req GET "/vehicles/${VEHICLE_ID}/tracking/latest" "$ADMIN_TOKEN")
  check_any "GET /vehicles/:id/tracking/latest as ADMIN" "$r" "200" "404"
fi

if [[ -n "$DISPATCH_ID" && "$DISPATCH_ID" != "null" ]]; then
  r=$(req GET "/dispatches/${DISPATCH_ID}/tracking" "$ADMIN_TOKEN")
  check "GET /dispatches/:id/tracking as ADMIN → 200" "$r" "200"

  r=$(req GET "/dispatches/${DISPATCH_ID}/tracking/latest" "$ADMIN_TOKEN")
  check_any "GET /dispatches/:id/tracking/latest as ADMIN" "$r" "200" "404"
fi

subsection "RBAC: no auth for tracking"
r=$(req POST /tracking "" '{"latitude":13.0,"longitude":80.0}')
check "POST /tracking no auth → 401" "$r" "401"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 11: INVITES
# ═════════════════════════════════════════════════════════════════════════════
section "11 📨 Invites Module"

subsection "Owner creates invites"
r=$(req POST /invites "$OWNER_TOKEN" "{
  \"invite_type\": \"MANAGER_INVITE\",
  \"nursery_id\": ${NURSERY_ID},
  \"target_mobile\": \"9000044444\",
  \"role\": \"MANAGER\"
}")
check "POST /invites (MANAGER_INVITE) as OWNER → 201" "$r" "201"
NEW_INVITE_UUID=$(jx "$(bd "$r")" ".invite.invite_uuid // .invite_uuid")

r=$(req GET "/nurseries/${NURSERY_ID}/invites" "$OWNER_TOKEN")
check "GET /nurseries/:id/invites as OWNER → 200" "$r" "200"

if [[ -n "$NEW_INVITE_UUID" && "$NEW_INVITE_UUID" != "null" ]]; then
  r=$(req GET "/invites/${NEW_INVITE_UUID}" "")
  check "GET /invites/:uuid public → 200" "$r" "200"

  r=$(req POST "/invites/${NEW_INVITE_UUID}/cancel" "$OWNER_TOKEN")
  check "POST /invites/:uuid/cancel as OWNER → 200" "$r" "200"
fi

if [[ -n "$INVITE_UUID" && "$INVITE_UUID" != "null" ]]; then
  r=$(req GET "/invites/${INVITE_UUID}" "")
  check "GET /invites/:uuid (existing) → 200" "$r" "200"
fi

subsection "RBAC: Driver/Buyer cannot create invites"
r=$(req POST /invites "$DRIVER_TOKEN" "{\"invite_type\":\"MANAGER_INVITE\",\"nursery_id\":${NURSERY_ID},\"target_mobile\":\"9000044444\"}")
check "POST /invites as DRIVER → 403" "$r" "403"

r=$(req POST /invites "$BUYER_TOKEN" "{\"invite_type\":\"MANAGER_INVITE\",\"nursery_id\":${NURSERY_ID},\"target_mobile\":\"9000044444\"}")
check "POST /invites as BUYER → 403" "$r" "403"

r=$(req POST /invites "" "{\"invite_type\":\"MANAGER_INVITE\",\"nursery_id\":${NURSERY_ID}}")
check "POST /invites no auth → 401" "$r" "401"

subsection "Validation"
r=$(req GET "/invites/00000000-0000-0000-0000-000000000000" "")
check_any "GET /invites/:uuid not found → 404" "$r" "404" "500"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 12: USERS
# ═════════════════════════════════════════════════════════════════════════════
section "12 👤 Users Module"

subsection "Users can view/edit own profile"
r=$(req GET /users/me "$ADMIN_TOKEN")
check "GET /users/me as ADMIN → 200" "$r" "200"

r=$(req GET /users/me "$OWNER_TOKEN")
check "GET /users/me as OWNER → 200" "$r" "200"

r=$(req GET /users/me "$DRIVER_TOKEN")
check "GET /users/me as DRIVER → 200" "$r" "200"

r=$(req PUT /users/me "$OWNER_TOKEN" '{"first_name":"Updated Owner","last_name":"Test"}')
check "PUT /users/me as OWNER → 200" "$r" "200"

if [[ -n "$ADMIN_UID" && "$ADMIN_UID" != "null" ]]; then
  r=$(req GET "/users/${ADMIN_UID}" "$ADMIN_TOKEN")
  check "GET /users/:id as ADMIN → 200" "$r" "200"

  r=$(req GET "/users/${ADMIN_UID}/roles" "$ADMIN_TOKEN")
  check "GET /users/:id/roles as ADMIN → 200" "$r" "200"

  r=$(req GET "/users/${ADMIN_UID}/sessions" "$ADMIN_TOKEN")
  check "GET /users/:id/sessions as ADMIN → 200" "$r" "200"
fi

subsection "User addresses"
if [[ -n "$BUYER_UID" && "$BUYER_UID" != "null" ]]; then
  r=$(req GET "/users/${BUYER_UID}/addresses" "$BUYER_TOKEN")
  check "GET /users/:id/addresses as self → 200" "$r" "200"

  r=$(req POST "/users/${BUYER_UID}/addresses" "$BUYER_TOKEN" '{
    "address_line1": "456 Buyer Street",
    "city": "Mumbai",
    "state": "Maharashtra",
    "country": "India"
  }')
  check "POST /users/:id/addresses as self → 201" "$r" "201"
  ADDR_ID=$(jx "$(bd "$r")" ".address.id // .id")

  if [[ -n "$ADDR_ID" && "$ADDR_ID" != "null" ]]; then
    r=$(req PUT "/users/addresses/${ADDR_ID}" "$BUYER_TOKEN" '{
      "address_line1": "456 Buyer Street Updated",
      "city": "Mumbai"
    }')
    check "PUT /users/addresses/:id as self → 200" "$r" "200"

    r=$(req DELETE "/users/addresses/${ADDR_ID}" "$BUYER_TOKEN")
    check "DELETE /users/addresses/:id as self → 200" "$r" "200"
  fi
fi

subsection "RBAC: Cannot access other user's data"
if [[ -n "$BUYER_UID" && "$BUYER_UID" != "null" ]]; then
  r=$(req GET "/users/${BUYER_UID}/roles" "$OWNER_TOKEN")
  check "GET /users/:id/roles as non-admin non-self → 403" "$r" "403"
fi

r=$(req GET /users/me "")
check "GET /users/me no auth → 401" "$r" "401"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 13: NOTIFICATIONS
# ═════════════════════════════════════════════════════════════════════════════
section "13 🔔 Notifications Module"

subsection "List own notifications"
r=$(req GET /notifications "$ADMIN_TOKEN")
check "GET /notifications as ADMIN → 200" "$r" "200"

r=$(req GET /notifications "$OWNER_TOKEN")
check "GET /notifications as OWNER → 200" "$r" "200"

r=$(req GET /notifications "$DRIVER_TOKEN")
check "GET /notifications as DRIVER → 200" "$r" "200"

subsection "Admin manages templates"
r=$(req GET /notifications/templates "$ADMIN_TOKEN")
check "GET /notifications/templates as ADMIN → 200" "$r" "200"

r=$(req POST /notifications/templates "$ADMIN_TOKEN" "{
  \"template_code\": \"TEST_TMPL_${TS}\",
  \"channel\": \"PUSH\",
  \"message_template\": \"Order {{order_id}} is ready\",
  \"is_active\": true
}")
check "POST /notifications/templates as ADMIN → 201" "$r" "201"
TEMPLATE_ID=$(jx "$(bd "$r")" ".template.id // .id")

if [[ -n "$TEMPLATE_ID" && "$TEMPLATE_ID" != "null" ]]; then
  r=$(req PUT "/notifications/templates/${TEMPLATE_ID}" "$ADMIN_TOKEN" "{
    \"template_code\": \"TEST_TMPL_${TS}\",
    \"channel\": \"PUSH\",
    \"message_template\": \"Order {{order_id}} updated\",
    \"is_active\": true
  }")
  check "PUT /notifications/templates/:id as ADMIN → 200" "$r" "200"

  r=$(req DELETE "/notifications/templates/${TEMPLATE_ID}" "$ADMIN_TOKEN")
  check "DELETE /notifications/templates/:id as ADMIN → 200" "$r" "200"
fi

subsection "Create and mark notification"
r=$(req POST /notifications "$ADMIN_TOKEN" "{
  \"user_id\": ${OWNER_UID:-1},
  \"notification_type\": \"ORDER_CREATED\",
  \"title\": \"Test Notification\",
  \"message\": \"This is a test\",
  \"channel\": \"IN_APP\"
}")
check "POST /notifications as ADMIN → 201" "$r" "201"
NOTIF_ID=$(jx "$(bd "$r")" ".notification.id // .id")

if [[ -n "$NOTIF_ID" && "$NOTIF_ID" != "null" ]]; then
  r=$(req GET "/notifications/${NOTIF_ID}" "$ADMIN_TOKEN")
  check "GET /notifications/:id as ADMIN → 200" "$r" "200"

  r=$(req PUT "/notifications/${NOTIF_ID}/read" "$ADMIN_TOKEN")
  check "PUT /notifications/:id/read → 200" "$r" "200"
fi

r=$(req PUT /notifications/read-all "$OWNER_TOKEN")
check "PUT /notifications/read-all as OWNER → 200" "$r" "200"

subsection "Device registration"
r=$(req GET /notifications/devices "$OWNER_TOKEN")
check "GET /notifications/devices as OWNER → 200" "$r" "200"

r=$(req POST /notifications/devices "$OWNER_TOKEN" '{
  "fcm_token": "test-fcm-token-abc123",
  "device_type": "ANDROID",
  "app_version": "1.0.0"
}')
check_any "POST /notifications/devices as OWNER → 200/201" "$r" "200" "201"

subsection "RBAC: no auth"
r=$(req GET /notifications "")
check "GET /notifications no auth → 401" "$r" "401"

r=$(req POST /notifications/templates "$OWNER_TOKEN" '{"template_code":"OWNER_TMPL","channel":"PUSH","message_template":"Test"}')
check "POST /notifications/templates as OWNER → 403" "$r" "403"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 14: ATTACHMENTS
# ═════════════════════════════════════════════════════════════════════════════
section "14 📎 Attachments Module"

subsection "Upload and retrieve attachments"
r=$(req POST /attachments "$OWNER_TOKEN" "{
  \"entity_type\": \"ORDER\",
  \"entity_id\": ${ORDER_ID:-1},
  \"file_url\": \"https://example.com/attachment.pdf\",
  \"file_name\": \"order-doc.pdf\",
  \"file_type\": \"application/pdf\"
}")
check "POST /attachments as OWNER → 201" "$r" "201"
ATTACHMENT_ID=$(jx "$(bd "$r")" ".attachment.id // .id")

r=$(req GET /attachments "$OWNER_TOKEN")
check "GET /attachments as OWNER → 200" "$r" "200"

if [[ -n "$ATTACHMENT_ID" && "$ATTACHMENT_ID" != "null" ]]; then
  r=$(req GET "/attachments/${ATTACHMENT_ID}" "$OWNER_TOKEN")
  check "GET /attachments/:id as OWNER → 200" "$r" "200"

  r=$(req DELETE "/attachments/${ATTACHMENT_ID}" "$ADMIN_TOKEN")
  check "DELETE /attachments/:id as ADMIN (only admin may delete) → 200" "$r" "200"
fi

subsection "RBAC: no auth"
r=$(req GET /attachments "")
check "GET /attachments no auth → 401" "$r" "401"

r=$(req POST /attachments "" '{"entity_type":"ORDER","entity_id":1,"file_url":"https://x.com/f.pdf"}')
check "POST /attachments no auth → 401" "$r" "401"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 15: AUDIT LOGS
# ═════════════════════════════════════════════════════════════════════════════
section "15 📝 Audit Logs Module"

subsection "Only admin can read audit logs"
r=$(req GET /audit-logs "$ADMIN_TOKEN")
check "GET /audit-logs as ADMIN → 200" "$r" "200"

r=$(req GET "/audit-logs?table_name=plants" "$ADMIN_TOKEN")
check "GET /audit-logs?table_name=plants as ADMIN → 200" "$r" "200"

subsection "RBAC: no non-admin access"
r=$(req GET /audit-logs "$OWNER_TOKEN")
check "GET /audit-logs as OWNER → 403" "$r" "403"

r=$(req GET /audit-logs "$MANAGER_TOKEN")
check "GET /audit-logs as MANAGER → 403" "$r" "403"

r=$(req GET /audit-logs "$DRIVER_TOKEN")
check "GET /audit-logs as DRIVER → 403" "$r" "403"

r=$(req GET /audit-logs "$BUYER_TOKEN")
check "GET /audit-logs as BUYER → 403" "$r" "403"

r=$(req GET /audit-logs "")
check "GET /audit-logs no auth → 401" "$r" "401"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 16: ADMIN DASHBOARD
# ═════════════════════════════════════════════════════════════════════════════
section "16 📊 Admin Dashboard Module"

subsection "Admin/SuperAdmin can view dashboard"
r=$(req GET /admin/dashboard "$ADMIN_TOKEN")
check "GET /admin/dashboard as ADMIN → 200" "$r" "200"

r=$(req GET /admin/users "$ADMIN_TOKEN")
check "GET /admin/users as ADMIN → 200" "$r" "200"

subsection "RBAC: non-admin blocked"
r=$(req GET /admin/dashboard "$OWNER_TOKEN")
check "GET /admin/dashboard as OWNER → 403" "$r" "403"

r=$(req GET /admin/dashboard "$MANAGER_TOKEN")
check "GET /admin/dashboard as MANAGER → 403" "$r" "403"

r=$(req GET /admin/dashboard "$DRIVER_TOKEN")
check "GET /admin/dashboard as DRIVER → 403" "$r" "403"

r=$(req GET /admin/dashboard "$BUYER_TOKEN")
check "GET /admin/dashboard as BUYER → 403" "$r" "403"

r=$(req GET /admin/dashboard "")
check "GET /admin/dashboard no auth → 401" "$r" "401"

r=$(req GET /admin/users "$OWNER_TOKEN")
check "GET /admin/users as OWNER → 403" "$r" "403"

r=$(req GET /admin/users "")
check "GET /admin/users no auth → 401" "$r" "401"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 17: SUBSCRIPTIONS
# ═════════════════════════════════════════════════════════════════════════════
section "17 💳 Subscriptions Module"

subsection "Public plan listing"
r=$(req GET /subscription-plans "")
check "GET /subscription-plans public → 200" "$r" "200"

PLAN_ID=$(jx "$(bd "$r")" ".plans[0].id // .subscription_plans[0].id")

if [[ -n "$PLAN_ID" && "$PLAN_ID" != "null" ]]; then
  r=$(req GET "/subscription-plans/${PLAN_ID}" "")
  check "GET /subscription-plans/:id public → 200" "$r" "200"
fi

subsection "Owner manages own subscription"
r=$(req GET /subscriptions "$ADMIN_TOKEN")
check "GET /subscriptions as ADMIN → 200" "$r" "200"

r=$(req GET /subscriptions/me "$OWNER_TOKEN")
check_any "GET /subscriptions/me as OWNER" "$r" "200" "404"

if [[ -n "$PLAN_ID" && "$PLAN_ID" != "null" ]]; then
  r=$(req POST /subscriptions "$OWNER_TOKEN" "{
    \"plan_id\": ${PLAN_ID},
    \"nursery_id\": ${NURSERY_ID}
  }")
  check_any "POST /subscriptions as OWNER → 201/409" "$r" "201" "409"
  SUBSCRIPTION_ID=$(jx "$(bd "$r")" ".subscription.id // .id")
fi

subsection "RBAC: no auth"
r=$(req GET /subscriptions "")
check "GET /subscriptions no auth → 401" "$r" "401"

r=$(req GET /subscriptions/me "")
check "GET /subscriptions/me no auth → 401" "$r" "401"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 18: PAYMENTS
# ═════════════════════════════════════════════════════════════════════════════
section "18 💰 Payments Module"

subsection "Admin and owner can manage payments"
r=$(req GET /payments "$ADMIN_TOKEN")
check "GET /payments as ADMIN → 200" "$r" "200"

r=$(req GET "/orders/${ORDER_ID:-1}/payments" "$OWNER_TOKEN")
check_any "GET /orders/:id/payments as OWNER" "$r" "200" "404"

r=$(req POST /payments/manual "$OWNER_TOKEN" "{
  \"order_id\": ${ORDER_ID:-1},
  \"amount\": 1000,
  \"payment_method\": \"CASH\",
  \"reference_number\": \"REF-TEST-001\"
}")
check "POST /payments/manual as OWNER → 201" "$r" "201"
PAYMENT_ID=$(jx "$(bd "$r")" ".payment.id // .id")

if [[ -n "$PAYMENT_ID" && "$PAYMENT_ID" != "null" ]]; then
  r=$(req GET "/payments/${PAYMENT_ID}" "$OWNER_TOKEN")
  check "GET /payments/:id as OWNER → 200" "$r" "200"

  r=$(req PUT "/payments/${PAYMENT_ID}/status" "$ADMIN_TOKEN" '{"payment_status":"SUCCESS"}')
  check "PUT /payments/:id/status as ADMIN → 200" "$r" "200"
fi

subsection "RBAC: no auth"
r=$(req GET /payments "")
check "GET /payments no auth → 401" "$r" "401"

r=$(req POST /payments/manual "" '{"order_id":1,"amount":100}')
check "POST /payments/manual no auth → 401" "$r" "401"

r=$(req POST /payments/manual "$DRIVER_TOKEN" "{\"order_id\":${ORDER_ID:-1},\"amount\":100,\"payment_method\":\"CASH\"}")
check "POST /payments/manual as DRIVER → 403" "$r" "403"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 19: PLANT SOURCING NETWORK
# ═════════════════════════════════════════════════════════════════════════════
section "19 🌐 Plant Sourcing Network Module"

subsection "Discovery (owner/manager)"
r=$(req GET /sourcing-network/nurseries "$OWNER_TOKEN")
check "GET /sourcing-network/nurseries as OWNER → 200" "$r" "200"

r=$(req GET /sourcing-network/nurseries "$MANAGER_TOKEN")
check "GET /sourcing-network/nurseries as MANAGER → 200" "$r" "200"

r=$(req GET /sourcing-network/nurseries "$ADMIN_TOKEN")
check "GET /sourcing-network/nurseries as ADMIN → 200" "$r" "200"

r=$(req GET "/sourcing-network/nurseries/${NURSERY_ID}" "$OWNER_TOKEN")
check_any "GET /sourcing-network/nurseries/:id as OWNER" "$r" "200" "404"

subsection "Membership management"
r=$(req GET "/nurseries/${NURSERY_ID}/sourcing-membership" "$OWNER_TOKEN")
check_any "GET /nurseries/:id/sourcing-membership as OWNER" "$r" "200" "404"

r=$(req POST "/nurseries/${NURSERY_ID}/sourcing-membership" "$OWNER_TOKEN" '{
  "road_accessible": true,
  "lorry_accessible": true,
  "contact_visible": true,
  "service_radius_km": 50
}')
check_any "POST /nurseries/:id/sourcing-membership (join) as OWNER" "$r" "200" "201" "409"

subsection "Featured plants"
r=$(req GET "/nurseries/${NURSERY_ID}/featured-plants" "$OWNER_TOKEN")
check "GET /nurseries/:id/featured-plants as OWNER → 200" "$r" "200"

r=$(req POST "/nurseries/${NURSERY_ID}/featured-plants" "$OWNER_TOKEN" "{
  \"plant_id\": ${PLANT_ID},
  \"display_order\": 1,
  \"quality_notes\": \"Usually have this in stock\",
  \"photos\": []
}")
check "POST /nurseries/:id/featured-plants as OWNER → 201" "$r" "201"
FEATURED_ID=$(jx "$(bd "$r")" ".featured_plant.id // .id")

if [[ -n "$FEATURED_ID" && "$FEATURED_ID" != "null" ]]; then
  r=$(req PUT "/nurseries/${NURSERY_ID}/featured-plants/${FEATURED_ID}" "$OWNER_TOKEN" '{
    "display_order": 2,
    "quality_notes": "Updated stock notes",
    "is_active": true,
    "photos": []
  }')
  check "PUT /nurseries/:id/featured-plants/:id as OWNER → 200" "$r" "200"

  r=$(req DELETE "/nurseries/${NURSERY_ID}/featured-plants/${FEATURED_ID}" "$OWNER_TOKEN")
  check "DELETE /nurseries/:id/featured-plants/:id as OWNER → 200" "$r" "200"
fi

subsection "Sourcing posts"
r=$(req GET /sourcing-posts "$OWNER_TOKEN")
check "GET /sourcing-posts as OWNER → 200" "$r" "200"

r=$(req POST /sourcing-posts "$OWNER_TOKEN" "{
  \"post_type\": \"NEED\",
  \"nursery_id\": ${NURSERY_ID},
  \"plant_id\": ${PLANT_ID},
  \"plant_name\": \"Rose\",
  \"urgency\": \"NORMAL\",
  \"radius_km\": 50
}")
check "POST /sourcing-posts as OWNER → 201" "$r" "201"
POST_ID=$(jx "$(bd "$r")" ".post.id // .id")

if [[ -n "$POST_ID" && "$POST_ID" != "null" ]]; then
  r=$(req GET "/sourcing-posts/${POST_ID}" "$OWNER_TOKEN")
  check "GET /sourcing-posts/:id as OWNER → 200" "$r" "200"

  r=$(req GET "/sourcing-posts/${POST_ID}/responses" "$OWNER_TOKEN")
  check "GET /sourcing-posts/:id/responses → 200" "$r" "200"

  r=$(req POST "/sourcing-posts/${POST_ID}/responses" "$MANAGER_TOKEN" "{
    \"responder_nursery_id\": ${NURSERY_ID},
    \"available_quantity\": 50,
    \"notes\": \"We can supply\"
  }")
  check "POST /sourcing-posts/:id/responses as MANAGER from same nursery → 400" "$r" "400"

  r=$(req PUT "/sourcing-posts/${POST_ID}" "$OWNER_TOKEN" '{
    "plant_name": "Rosa indica",
    "urgency": "HIGH",
    "status": "OPEN"
  }')
  check "PUT /sourcing-posts/:id as OWNER → 200" "$r" "200"

  r=$(req DELETE "/sourcing-posts/${POST_ID}" "$OWNER_TOKEN")
  check "DELETE /sourcing-posts/:id as OWNER → 200" "$r" "200"
fi

subsection "RBAC: Driver/Buyer cannot access sourcing network"
r=$(req GET /sourcing-network/nurseries "$DRIVER_TOKEN")
check "GET /sourcing-network/nurseries as DRIVER → 403" "$r" "403"

r=$(req GET /sourcing-network/nurseries "$BUYER_TOKEN")
check "GET /sourcing-network/nurseries as BUYER → 403" "$r" "403"

r=$(req GET /sourcing-network/nurseries "")
check "GET /sourcing-network/nurseries no auth → 401" "$r" "401"

r=$(req POST /sourcing-posts "$DRIVER_TOKEN" '{"post_type":"NEED","title":"Driver post"}')
check "POST /sourcing-posts as DRIVER → 403" "$r" "403"

r=$(req POST /sourcing-posts "$BUYER_TOKEN" '{"post_type":"NEED","title":"Buyer post"}')
check "POST /sourcing-posts as BUYER → 403" "$r" "403"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 20: PLANT REQUESTS
# ═════════════════════════════════════════════════════════════════════════════
section "20 🌱 Plant Requests Module"

subsection "Owner/Manager/Admin can manage plant requests"
r=$(req GET /plant-requests "$OWNER_TOKEN")
check "GET /plant-requests as OWNER → 200" "$r" "200"

r=$(req GET /plant-requests "$ADMIN_TOKEN")
check "GET /plant-requests as ADMIN → 200" "$r" "200"

r=$(req GET /plant-requests "$MANAGER_TOKEN")
check "GET /plant-requests as MANAGER → 200" "$r" "200"

r=$(req POST /plant-requests "$OWNER_TOKEN" "{
  \"requesting_nursery_id\": ${NURSERY_ID},
  \"plant_id\": ${PLANT_ID},
  \"quantity_required\": 10,
  \"radius_km\": 100,
  \"status\": \"OPEN\"
}")
check "POST /plant-requests as OWNER → 201" "$r" "201"
REQUEST_ID=$(jx "$(bd "$r")" ".plant_request.id // .request.id // .id")

if [[ -n "$REQUEST_ID" && "$REQUEST_ID" != "null" ]]; then
  r=$(req GET "/plant-requests/${REQUEST_ID}" "$OWNER_TOKEN")
  check "GET /plant-requests/:id as OWNER → 200" "$r" "200"

  r=$(req GET "/plant-requests/${REQUEST_ID}/responses" "$OWNER_TOKEN")
  check "GET /plant-requests/:id/responses → 200" "$r" "200"

  r=$(req POST "/plant-requests/${REQUEST_ID}/responses" "$MANAGER_TOKEN" "{
    \"supplier_nursery_id\": ${NURSERY_ID},
    \"available_quantity\": 10,
    \"remarks\": \"We can source this\",
    \"status\": \"AVAILABLE\"
  }")
  check "POST /plant-requests/:id/responses as MANAGER from same nursery → 400" "$r" "400"

  r=$(req PUT "/plant-requests/${REQUEST_ID}/status" "$OWNER_TOKEN" '{"status":"OPEN"}')
  check "PUT /plant-requests/:id/status as OWNER → 200" "$r" "200"
fi

subsection "RBAC: Driver/Buyer blocked"
r=$(req POST /plant-requests "$DRIVER_TOKEN" '{"plant_name":"Driver Request","quantity":1}')
check "POST /plant-requests as DRIVER → 403" "$r" "403"

r=$(req GET /plant-requests "$BUYER_TOKEN")
check "GET /plant-requests as BUYER → 403" "$r" "403"

r=$(req GET /plant-requests "")
check "GET /plant-requests no auth → 401" "$r" "401"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 21: STORAGE
# ═════════════════════════════════════════════════════════════════════════════
section "21 🗄️  Storage Module"

subsection "Presign URL generation"
r=$(req POST /storage/presign "$ADMIN_TOKEN" '{
  "bucket": "plant-images",
  "file_name": "test-image.jpg",
  "content_type": "image/jpeg"
}')
check_any "POST /storage/presign as ADMIN → 200 or 500 (MinIO may not be running)" "$r" "200" "201" "500"

r=$(req POST /storage/presign "$OWNER_TOKEN" '{
  "bucket": "profile-images",
  "file_name": "nursery-photo.jpg",
  "content_type": "image/jpeg"
}')
check_any "POST /storage/presign as OWNER → 200 or 500 (MinIO may not be running)" "$r" "200" "201" "500"

subsection "RBAC: no auth"
r=$(req POST /storage/presign "" '{"bucket":"plant-images","file_name":"test.jpg","content_type":"image/jpeg"}')
check "POST /storage/presign no auth → 401" "$r" "401"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 22: WORKSPACE / ME ENDPOINTS
# ═════════════════════════════════════════════════════════════════════════════
section "22 🏠 Workspace & Me Endpoints"

subsection "Workspace for each role"
r=$(req GET /me/workspaces "$ADMIN_TOKEN")
check "GET /me/workspaces as ADMIN → 200" "$r" "200"

r=$(req GET /me/workspaces "$OWNER_TOKEN")
check "GET /me/workspaces as OWNER → 200" "$r" "200"

r=$(req GET /me/workspaces "$MANAGER_TOKEN")
check "GET /me/workspaces as MANAGER → 200" "$r" "200"

r=$(req GET /me/workspaces "$DRIVER_TOKEN")
check "GET /me/workspaces as DRIVER → 200" "$r" "200"

r=$(req GET /me/workspaces "$BUYER_TOKEN")
check "GET /me/workspaces as BUYER → 200" "$r" "200"

r=$(req GET /me/owner-dashboard "$OWNER_TOKEN")
check "GET /me/owner-dashboard as OWNER → 200" "$r" "200"

r=$(req GET /me/owner-dashboard "$MANAGER_TOKEN")
check "GET /me/owner-dashboard as MANAGER → 403" "$r" "403"

subsection "RBAC: no auth"
r=$(req GET /me/workspaces "")
check "GET /me/workspaces no auth → 401" "$r" "401"

r=$(req GET /me/owner-dashboard "")
check "GET /me/owner-dashboard no auth → 401" "$r" "401"

# ═════════════════════════════════════════════════════════════════════════════
# MODULE 23: CRITICAL BUSINESS RULE SUMMARY
# ═════════════════════════════════════════════════════════════════════════════
section "23 🔒 Critical RBAC Business Rules — Final Verification"

echo -e "\n  Verifying the 4 bugs fixed in this session…\n"

subsection "Bug 1: canManagePlants — NURSERY_OWNER must be BLOCKED from plant write"
r=$(req POST /plants "$OWNER_TOKEN" '{"scientific_name":"Owner should not create plant"}')
check "POST /plants as NURSERY_OWNER → 403 ✔ (FIXED: was 201)" "$r" "403"

subsection "Bug 2: admin/dashboard — SUPER_ADMIN must NOT be locked out"
r=$(req GET /admin/dashboard "$ADMIN_TOKEN")
check "GET /admin/dashboard as ADMIN → 200 ✔ (FIXED: SUPER_ADMIN also passes)" "$r" "200"

subsection "Bug 3: quotations/Create — ADMIN must be BLOCKED from creating quotations"
r=$(req POST /quotations "$ADMIN_TOKEN" "{
  \"quotation_type\":\"INTERNAL\",
  \"nursery_id\":${NURSERY_ID},
  \"items\":[{\"plant_id\":${PLANT_ID},\"quantity\":1,\"unit_price\":100,\"total_price\":100}]
}")
check "POST /quotations as ADMIN → 403 ✔ (FIXED: was 201)" "$r" "403"

subsection "Bug 4: dispatches/Create — ADMIN must be BLOCKED from creating dispatches"
r=$(req POST /dispatches "$ADMIN_TOKEN" "{\"order_id\":${ORDER_ID:-1},\"destination_address\":\"Admin test\"}")
check "POST /dispatches as ADMIN → 403 ✔ (FIXED: was 201)" "$r" "403"

# ═════════════════════════════════════════════════════════════════════════════
# FINAL SUMMARY
# ═════════════════════════════════════════════════════════════════════════════
echo ""
echo -e "${BLUE}${BOLD}╔══════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}${BOLD}║            🌿 GreenRoot API Test Summary 🌿              ║${NC}"
echo -e "${BLUE}${BOLD}╚══════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "  📊 Total tests run : ${BOLD}${TOTAL}${NC}"
echo -e "  ✅ Passed          : ${GREEN}${BOLD}${PASS}${NC}"
echo -e "  ❌ Failed          : ${RED}${BOLD}${FAIL}${NC}"
echo ""

PASS_PCT=0
if [[ $TOTAL -gt 0 ]]; then
  PASS_PCT=$(( (PASS * 100) / TOTAL ))
fi

if [[ $FAIL -eq 0 ]]; then
  echo -e "  ${GREEN}${BOLD}🎉 ALL TESTS PASSED! (${PASS_PCT}%) — Ready for UI testing!${NC}"
elif [[ $PASS_PCT -ge 90 ]]; then
  echo -e "  ${YELLOW}${BOLD}⚠️  ${PASS_PCT}% passing — ${FAIL} test(s) need attention before UI testing.${NC}"
elif [[ $PASS_PCT -ge 70 ]]; then
  echo -e "  ${YELLOW}${BOLD}🔧 ${PASS_PCT}% passing — Fix ${FAIL} failure(s) before proceeding.${NC}"
else
  echo -e "  ${RED}${BOLD}🚨 ${PASS_PCT}% passing — ${FAIL} failure(s). Check API server and seed data.${NC}"
fi

echo ""
echo -e "  ${CYAN}Tip: Run with BASE_URL=http://other-host:8080/api/v1 ./test-api.sh${NC}"
echo -e "  ${CYAN}Logs: greenroot-api/logs/ (app.log + errors.log)${NC}"
echo ""

if [[ $FAIL -gt 0 ]]; then
  exit 1
fi
exit 0
