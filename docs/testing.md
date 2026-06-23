# GreenRoot API — Testing Guide

## Overview

Two test layers exist:

| Layer | Command | Database | Safe for local DB? |
|---|---|---|---|
| Smoke suite | `make smoke` | Uses existing local `greenroot` | ✅ Yes — read-only |
| Integration suite | `make integration` | Disposable temp DB | ✅ Yes — isolated |

---

## Smoke Tests

Non-destructive HTTP checks against a running API.

### Run

```bash
# Start API first
DATABASE_URL='postgres:///greenroot?host=/tmp' \
JWT_SECRET='local-dev-change-me' \
LOG_DIR='/tmp/gr-logs' \
go run ./cmd/api

# Then in another terminal
make smoke
```

### Overrides

```bash
SMOKE_BASE_URL=http://127.0.0.1:8080 make smoke
SMOKE_MOBILE=9000000777 make smoke
```

### Results

```text
test-log/smoke/results.log
```

### What It Covers

- Health endpoints (`/health`, `/healthz`, `/readyz`)
- OpenAPI route coverage checks
- OTP login with mocked `123456`
- JWT-protected route access
- Public catalog reads
- User context reads
- Notification device upsert
- Admin/audit authorization behavior

---

## Integration Tests

Disposable-database full-flow HTTP test suite.

### Run

```bash
make integration
```

### What It Does

1. Creates temporary PostgreSQL database (`greenroot_integration_test`)
2. Applies all migrations
3. Loads the infra seed
4. Creates dedicated role test users (see table below)
5. Starts API on a local test port
6. Runs role checks + full-flow HTTP assertions
7. Drops the temporary database

### Test Users

| Role | Mobile |
|---|---|
| `ADMIN` | `9100000001` |
| `BUYER` | `9100000002` |
| `NURSERY_OWNER` | `9100000003` |
| `DRIVER` | `9100000004` |

OTP for all: `123456`

### Results

```text
test-log/integration/
├── results.log
├── api-process.log
└── runtime/
    └── YYYY-MM/YYYY-MM-DD/
        ├── app.log
        └── errors.log
```

### Covered Flows

| Area | Flows Tested |
|---|---|
| Auth | Login, logout, role-based access |
| RBAC | Admin, buyer, nursery owner, driver access checks |
| Plants | Create, list, detail, update, image, care-guide |
| Nurseries | Create, list, update, address, users |
| Inventory | Create, list, update |
| Plant Requests | Create, list, respond |
| Orders | Create, list, status update |
| Payments | Manual payment record |
| Subscriptions | Create, me, cancel |
| Vehicles/Drivers | Create |
| Tracking | Driver location, tracking point |
| Dispatches | Create, list |
| Attachments | Create, list, delete |
| Notifications | Device, template, create, list |
| Audit/Admin | Access control verification |

---

## Backlog — Tests Not Yet Written

| Flow | Missing Tests |
|---|---|
| Auth | Invalid OTP, expired token, refresh reuse |
| User Profile | Update profile, create address, list sessions |
| Plants | Search/filter assertions, soft-delete behavior |
| Nurseries | Remove user, address update/delete |
| Inventory | List by nursery, list by plant, out-of-stock transitions |
| Plant Requests | Update response, insufficient inventory negative |
| Orders | Add/update/delete item, buyer forbidden cases |
| Payments | Subscription gateway metadata, failed/refunded status |
| Subscriptions | Renew, admin status updates |
| Dispatches | Add item, delivery status transitions |
| Tracking | Latest/history by driver and dispatch |
| Notifications | Mark read, mark all read, delete template |
| Attachments | Non-admin delete forbidden |
| Audit/Admin | Dashboard count assertions |

---

## Test Rules

- Always use a disposable database for integration tests
- Never reset or delete the developer's local `greenroot` database
- Keep route contracts stable — integration tests catch breaking changes
- Apply migrations with `make migrate-up` in test harness
- Load seed only when a test needs baseline data
