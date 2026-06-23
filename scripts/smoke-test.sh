#!/usr/bin/env sh
set -eu

# Runs the non-destructive HTTP smoke suite against an already running API.
BASE_URL="${SMOKE_BASE_URL:-http://127.0.0.1:8080}"
MOBILE="${SMOKE_MOBILE:-9000000777}"
TEST_LOG_DIR="${TEST_LOG_DIR:-test-log}"
SMOKE_TEST_LOG_DIR="$TEST_LOG_DIR/smoke"
RESULT_LOG="$SMOKE_TEST_LOG_DIR/results.log"

mkdir -p "$SMOKE_TEST_LOG_DIR"
: > "$RESULT_LOG"

if go run ./cmd/smoke -base-url "$BASE_URL" -mobile "$MOBILE" >"$RESULT_LOG" 2>&1; then
  cat "$RESULT_LOG"
else
  cat "$RESULT_LOG"
  echo "smoke results log: $RESULT_LOG" >&2
  exit 1
fi
