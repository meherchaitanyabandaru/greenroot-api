# GreenRoot — API Reference

> Last updated: 2026-06-28

---

## Stack

Go · chi router · PostgreSQL · JWT/OTP auth (mobile + `123456` mock OTP in dev)

Run locally (port 8080):
```bash
cd greenroot-api
DATABASE_URL='postgres:///greenroot?host=/tmp' JWT_SECRET='local-dev-change-me' LOG_DIR='/tmp/gr-logs' go run ./cmd/api
```

Swagger: `http://localhost:8080/swagger/`  
OpenAPI spec (source of truth): `docs/swagger/openapi.yaml`

Current registered route count from chi route definitions:

| Scope | Count |
|---|---:|
| `/api/v1` module routes | 175 |
| Health routes | 3 |
| Docs/Swagger routes | 4 |
| **Total registered APIs** | **182** |

---

## Architecture

```
Request → Handler → Service → Repository → PostgreSQL
```

| Layer | Responsibility |
|---|---|
| Handler | Parse request/params, return standard JSON |
| Service | Business rules, role checks, workflow decisions |
| Repository | SQL only — no business logic |

**Rules:**
- Modules do NOT call other modules' repositories directly
- Shared infrastructure belongs in `internal/common`, `internal/database`, `internal/middleware`, or top-level `platform`
- V1 is a **modular monolith** — no microservices, event buses, or Kafka
- All protected handlers use `internal/common/authctx` for JWT actor extraction — never reimplement token parsing

### Module File Structure
```
internal/modules/<name>/
├── handler.go
├── routes.go
├── service.go
├── repository.go
├── model.go
└── handler_test.go
```

---

## Authentication

OTP-based login. JWT access + refresh tokens. Sessions tracked per device.

Key routes:
```
POST /api/v1/auth/send-otp
POST /api/v1/auth/verify-otp
POST /api/v1/auth/refresh-token
POST /api/v1/auth/logout
GET  /api/v1/me/workspaces
GET  /api/v1/me/owner-dashboard
```

Dev OTP: `123456` hardcoded (mock).

Dev credentials:

| Mobile | Role |
|---|---|
| `9000000000` | Admin + Super Admin |
| `9100000000` | Nursery Owner |
| `9200000000` | Manager |
| `9300000000` | Buyer |
| `9400000000` | Driver |

---

## RBAC — Roles

| Role | Description |
|---|---|
| `SUPER_ADMIN` | Full platform access including admin management |
| `ADMIN` | Platform ops, catalog, audit, dashboard |
| `NURSERY_OWNER` | Owns one nursery; manages inventory, orders, managers, customers |
| `MANAGER` | Works under one nursery (exclusive — cannot simultaneously be owner) |
| `DRIVER` | Independent; joins trips via UUID/QR; never owned by any nursery |
| `BUYER` | Read own orders + track delivery; auto-created when staff places order |

### Route Policy

| Area | Public | Admin | Nursery Owner | Manager | Driver | Buyer |
|---|---|---|---|---|---|---|
| Health | ✅ | — | — | — | — | — |
| Auth | partial | — | — | — | — | — |
| Users | — | global | own/member | own | own | own |
| Plants | read | write | read | read | read | read |
| Nurseries | read | write/global | own nursery | own nursery | — | read |
| Inventory | — | global | own nursery | own nursery | — | — |
| Plant Requests | — | global | nursery B2B | nursery B2B | — | — |
| Orders | — | global | own nursery | own nursery | — | own orders |
| Payments | — | global | order-linked | — | — | own |
| Subscriptions | plans | global | own | — | — | — |
| Vehicles | — | global write | — | — | assigned | — |
| Drivers | — | global write | — | — | own profile | — |
| Dispatches | — | global | own nursery | own nursery | assigned | own orders |
| Tracking | — | global | own nursery | own nursery | assigned | own orders |
| Notifications | — | global/templates | own | own | own | own |
| Audit | — | ✅ | — | — | — | — |
| Admin | — | ✅ | — | — | — | — |

**Authorization lives in the service layer.** Handlers only extract actor context.

---

## Business Rules (API Enforced)

See `BUSINESS_RULES.md` for the full rule set. Key enforcements:

- **One owner → one nursery.** Shared/partner ownership not supported.
- **Manager exclusivity.** MANAGER_INVITE rejected if user owns nursery. NURSERY_ONBOARDING_INVITE rejected if user is manager. `uq_manager_one_active_nursery` DB constraint enforces single active nursery per user.
- **Driver independence.** Drivers join individual trips via UUID/QR; no nursery ownership.
- **Order state machine.** `PENDING → CONFIRMED → LOADING → LOADED/PARTIALLY_FULFILLED → COMPLETED`. Cancel goes via `POST /orders/:id/cancel` (not a status update). Invalid direct transitions are blocked.
- **Order item editing window.** Items (add/edit/remove) allowed in `PENDING`, `CONFIRMED`, `LOADING`. Locked at `LOADED`, `PARTIALLY_FULFILLED`, `COMPLETED`.
- **Loaded quantities.** `PUT /orders/:id/items/:itemId/loaded-quantity` sets actual loaded amount during `LOADING`. If any item is under-loaded at `complete-loading`, order moves to `PARTIALLY_FULFILLED` and invoice is recalculated.
- **Cancel guards.** `LOADED` and `PARTIALLY_FULFILLED` orders cannot be cancelled. Buyers can cancel their own `PENDING` orders without management access.
- **Delete guards.** Only `PENDING` orders can be hard-deleted.
- **Nursery registration.** Any authenticated user (except drivers) may register a nursery. Drivers get `403`.
- **Audit trail mandatory.** Every significant action writes an audit log — immutable.
- **Nursery activation requires approval.** Until `ACTIVE`, owners cannot create quotations, orders, or send invites.
- **Invite side-effects.** MANAGER_INVITE accepted → inserts `nursery_users` row. NURSERY_ONBOARDING_INVITE accepted → grants NURSERY_OWNER role in `user_roles`.

---

## Module Status (All Complete)

| Module | Main Routes |
|---|---|
| auth | `POST /auth/send-otp`, `verify-otp`, `refresh-token`, `logout` · `GET /auth/me` |
| users | `GET/PUT /users/:id` · addresses, roles, sessions |
| plants | `GET/POST /plants` · `GET/PUT/DELETE /plants/:id` · images, care-guide, categories |
| nurseries | `GET/POST /nurseries` · `GET/PUT/DELETE /nurseries/:id` · addresses, users |
| inventory | `GET/POST /inventory` · `GET/PUT/DELETE /inventory/:id` · by nursery, by plant |
| requests | `GET/POST /plant-requests` · `GET/PUT/DELETE /plant-requests/:id` · responses, cancel |
| orders | `GET/POST /orders` · `GET/DELETE /orders/:id` · status, items, loaded-quantity, start-loading, complete-loading, cancel, assign-manager |
| payments | `GET/POST /payments` · manual, status · by order, by subscription |
| subscriptions | `GET /subscription-plans` · `GET/POST /subscriptions` · renew, cancel, status |
| vehicles | `GET/POST /vehicles` · `GET/PUT/DELETE /vehicles/:id` |
| drivers | `GET/POST /drivers` · `GET/PUT/DELETE /drivers/:id` · location |
| dispatches | `GET/POST /dispatches` · `GET /dispatches/:id` · status, items · by order |
| tracking | `POST /tracking` · vehicle/driver/dispatch latest + history |
| notifications | `GET/POST /notifications` · read, delete · devices, templates |
| attachments | `GET/POST /attachments` · `GET/DELETE /attachments/:id` |
| audit | `GET /audit-logs` |
| admin | `GET /admin/dashboard` · `GET /admin/users` |
| health | `GET /health` · `/healthz` · `/readyz` |
| quotations | `POST /quotations/:id/approve` · `POST /quotations/:id/convert-to-order` |
| invites | `POST /invites` · `GET /invites/:uuid` · accept, cancel · nursery invites + managers |
| me/workspaces | `GET /me/workspaces` — returns PERSONAL, OWNED_NURSERY, MANAGER_NURSERY, DRIVER workspaces |

---

## Registered API Inventory

All module routes below are mounted under `/api/v1` unless noted.

| Module | Count | Registered routes |
|---|---:|---|
| Health | 3 | `GET /health`, `GET /healthz`, `GET /readyz` |
| Docs/Swagger | 4 | `GET /openapi.yaml`, `GET /swagger`, `GET /swagger/`, `GET /swagger/index.html` |
| Auth | 6 | `POST /api/v1/auth/send-otp`, `POST /api/v1/auth/verify-otp`, `POST /api/v1/auth/refresh-token`, `POST /api/v1/auth/logout`, `GET /api/v1/me/workspaces`, `GET /api/v1/me/owner-dashboard` |
| Admin | 2 | `GET /api/v1/admin/dashboard`, `GET /api/v1/admin/users` |
| Users | 9 | `GET /api/v1/users/me`, `PUT /api/v1/users/me`, `GET /api/v1/users/{id}`, `GET /api/v1/users/{id}/addresses`, `POST /api/v1/users/{id}/addresses`, `PUT /api/v1/users/addresses/{addressId}`, `DELETE /api/v1/users/addresses/{addressId}`, `GET /api/v1/users/{id}/roles`, `GET /api/v1/users/{id}/sessions` |
| Plants | 13 | `GET /api/v1/plants`, `POST /api/v1/plants`, `GET /api/v1/plants/sizes`, `GET /api/v1/plants/categories`, `POST /api/v1/plants/categories`, `PUT /api/v1/plants/categories/{categoryId}`, `DELETE /api/v1/plants/categories/{categoryId}`, `GET /api/v1/plants/names`, `GET /api/v1/plants/{id}`, `PUT /api/v1/plants/{id}`, `DELETE /api/v1/plants/{id}`, `POST /api/v1/plants/{id}/images`, `GET /api/v1/plants/{id}/care-guide` |
| Nurseries | 19 | `GET /api/v1/nurseries`, `POST /api/v1/nurseries`, `GET /api/v1/nurseries/mine`, `GET /api/v1/nurseries/owned`, `GET /api/v1/nurseries/{id}`, `PUT /api/v1/nurseries/{id}`, `PUT /api/v1/nurseries/{id}/status`, `DELETE /api/v1/nurseries/{id}`, `GET /api/v1/nurseries/{id}/addresses`, `POST /api/v1/nurseries/{id}/addresses`, `PUT /api/v1/nurseries/addresses/{addressId}`, `DELETE /api/v1/nurseries/addresses/{addressId}`, `GET /api/v1/nurseries/{id}/managers`, `POST /api/v1/nurseries/{id}/managers`, `DELETE /api/v1/nurseries/{id}/managers/{userId}`, `GET /api/v1/nurseries/{id}/drivers`, `POST /api/v1/nurseries/{id}/drivers`, `POST /api/v1/nurseries/{id}/drivers/{driverUserId}/approve`, `GET /api/v1/nurseries/{id}/customers` |
| Inventory | 7 | `GET /api/v1/inventory`, `POST /api/v1/inventory`, `GET /api/v1/inventory/{id}`, `PUT /api/v1/inventory/{id}`, `DELETE /api/v1/inventory/{id}`, `GET /api/v1/nurseries/{nurseryId}/inventory`, `GET /api/v1/plants/{plantId}/inventory` |
| Plant Requests | 9 | `GET /api/v1/plant-requests`, `POST /api/v1/plant-requests`, `GET /api/v1/plant-requests/{id}`, `PUT /api/v1/plant-requests/{id}`, `PUT /api/v1/plant-requests/{id}/status`, `DELETE /api/v1/plant-requests/{id}`, `GET /api/v1/plant-requests/{id}/responses`, `POST /api/v1/plant-requests/{id}/responses`, `PUT /api/v1/plant-requests/responses/{responseId}` |
| Orders | 14 | `GET /api/v1/orders`, `POST /api/v1/orders`, `GET /api/v1/orders/{id}`, `PUT /api/v1/orders/{id}/status`, `DELETE /api/v1/orders/{id}`, `POST /api/v1/orders/{id}/start-loading`, `POST /api/v1/orders/{id}/complete-loading`, `POST /api/v1/orders/{id}/cancel`, `POST /api/v1/orders/{id}/assign-manager`, `GET /api/v1/orders/{id}/items`, `POST /api/v1/orders/{id}/items`, `PUT /api/v1/orders/{id}/items/{itemId}`, `DELETE /api/v1/orders/{id}/items/{itemId}`, `PUT /api/v1/orders/{id}/items/{itemId}/loaded-quantity` |
| Quotations | 13 | `GET /api/v1/quotations`, `POST /api/v1/quotations`, `GET /api/v1/quotations/{id}`, `PUT /api/v1/quotations/{id}`, `DELETE /api/v1/quotations/{id}`, `POST /api/v1/quotations/{id}/assign-manager`, `DELETE /api/v1/quotations/{id}/assign-manager`, `POST /api/v1/quotations/{id}/approve`, `POST /api/v1/quotations/{id}/recall`, `POST /api/v1/quotations/{id}/convert-to-order`, `POST /api/v1/quotations/{id}/buyer-accept`, `POST /api/v1/quotations/{id}/buyer-reject`, `POST /api/v1/quotations/{id}/record-download` |
| Payments | 6 | `GET /api/v1/payments`, `POST /api/v1/payments/manual`, `GET /api/v1/payments/{id}`, `PUT /api/v1/payments/{id}/status`, `GET /api/v1/orders/{orderId}/payments`, `GET /api/v1/subscriptions/{subscriptionId}/payments` |
| Subscriptions | 9 | `GET /api/v1/subscription-plans`, `GET /api/v1/subscription-plans/{id}`, `GET /api/v1/subscriptions`, `POST /api/v1/subscriptions`, `GET /api/v1/subscriptions/me`, `GET /api/v1/subscriptions/{id}`, `PUT /api/v1/subscriptions/{id}/status`, `POST /api/v1/subscriptions/{id}/renew`, `POST /api/v1/subscriptions/{id}/cancel` |
| Dispatches | 10 | `GET /api/v1/track/{uuid}`, `GET /api/v1/dispatches`, `POST /api/v1/dispatches`, `GET /api/v1/dispatches/code/{code}`, `GET /api/v1/dispatches/{id}`, `PUT /api/v1/dispatches/{id}/status`, `POST /api/v1/dispatches/{id}/accept`, `POST /api/v1/dispatches/{id}/items`, `POST /api/v1/dispatches/{id}/trip-events`, `GET /api/v1/orders/{orderId}/dispatches` |
| Drivers | 9 | `POST /api/v1/drivers/apply`, `GET /api/v1/drivers/me`, `GET /api/v1/drivers`, `POST /api/v1/drivers`, `GET /api/v1/drivers/{id}`, `PUT /api/v1/drivers/{id}`, `DELETE /api/v1/drivers/{id}`, `POST /api/v1/drivers/{id}/approve`, `POST /api/v1/drivers/{id}/location` |
| Vehicles | 5 | `GET /api/v1/vehicles`, `POST /api/v1/vehicles`, `GET /api/v1/vehicles/{id}`, `PUT /api/v1/vehicles/{id}`, `DELETE /api/v1/vehicles/{id}` |
| Tracking | 7 | `POST /api/v1/tracking`, `GET /api/v1/dispatches/{dispatchId}/tracking`, `GET /api/v1/dispatches/{dispatchId}/tracking/latest`, `GET /api/v1/drivers/{driverId}/tracking`, `GET /api/v1/drivers/{driverId}/tracking/latest`, `GET /api/v1/vehicles/{vehicleId}/tracking`, `GET /api/v1/vehicles/{vehicleId}/tracking/latest` |
| Notifications | 13 | `GET /api/v1/notifications`, `POST /api/v1/notifications`, `PUT /api/v1/notifications/read-all`, `GET /api/v1/notifications/devices`, `POST /api/v1/notifications/devices`, `DELETE /api/v1/notifications/devices/{id}`, `GET /api/v1/notifications/templates`, `POST /api/v1/notifications/templates`, `PUT /api/v1/notifications/templates/{id}`, `DELETE /api/v1/notifications/templates/{id}`, `GET /api/v1/notifications/{id}`, `PUT /api/v1/notifications/{id}/read`, `DELETE /api/v1/notifications/{id}` |
| Attachments | 4 | `GET /api/v1/attachments`, `POST /api/v1/attachments`, `GET /api/v1/attachments/{id}`, `DELETE /api/v1/attachments/{id}` |
| Invites | 5 | `POST /api/v1/invites`, `GET /api/v1/invites/{uuid}`, `POST /api/v1/invites/{uuid}/accept`, `POST /api/v1/invites/{uuid}/cancel`, `GET /api/v1/nurseries/{nurseryId}/invites` |
| Sourcing | 17 | `GET /api/v1/nurseries/{nurseryId}/sourcing-membership`, `POST /api/v1/nurseries/{nurseryId}/sourcing-membership`, `DELETE /api/v1/nurseries/{nurseryId}/sourcing-membership`, `GET /api/v1/nurseries/{nurseryId}/featured-plants`, `POST /api/v1/nurseries/{nurseryId}/featured-plants`, `PUT /api/v1/nurseries/{nurseryId}/featured-plants/{featuredId}`, `DELETE /api/v1/nurseries/{nurseryId}/featured-plants/{featuredId}`, `GET /api/v1/sourcing-network/nurseries`, `GET /api/v1/sourcing-network/nurseries/{nurseryId}`, `GET /api/v1/sourcing-posts`, `POST /api/v1/sourcing-posts`, `GET /api/v1/sourcing-posts/{id}`, `PUT /api/v1/sourcing-posts/{id}`, `DELETE /api/v1/sourcing-posts/{id}`, `GET /api/v1/sourcing-posts/{id}/responses`, `POST /api/v1/sourcing-posts/{id}/responses`, `PUT /api/v1/sourcing-posts/{id}/responses/{responseId}` |
| Storage | 1 | `POST /api/v1/storage/presign` |
| Audit | 1 | `GET /api/v1/audit-logs` |

---

## Provider Status (Intentionally Mocked for V1)

| Provider | Status |
|---|---|
| FCM (Push notifications) | Mock — real credentials needed |
| S3 File Storage | Mock — pre-signed URL logic pending |
| Razorpay/PayU Payments | Mock — gateway capture pending |
| OTP Provider | Mock — `123456` hardcoded |

---

## Testing

| Layer | Command | Notes |
|---|---|---|
| Smoke | `make smoke` | Non-destructive, uses local `greenroot` DB |
| Integration | `make integration` | Disposable DB, applies migrations + seed, runs full flows |

Dev / integration test users (OTP `123456` for all):
| Role | Mobile | Name |
|---|---|---|
| ADMIN + SUPER_ADMIN | `9000000000` | GreenRoot Admin |
| NURSERY_OWNER | `9100000000` | Priya Owner |
| MANAGER | `9200000000` | Gumastha Manager |
| BUYER | `9300000000` | Ravi Buyer |
| DRIVER | `9400000000` | Raju Driver |

Results: `test-log/smoke/results.log` and `test-log/integration/`.

---

## Pre-Production Hardening Backlog

- [ ] Rate limiting on all routes
- [ ] Request size limits
- [ ] Security headers (HSTS, X-Frame-Options)
- [ ] CORS policy review per environment
- [ ] Login brute-force protection
- [ ] RBAC enforcement audit — all routes
- [ ] Real FCM, S3, Razorpay/PayU integrations
- [ ] Expand integration tests — edge cases + negative paths
- [ ] CI/CD: `gofmt`, `go vet ./...`, `go test ./...`, `go build`, OpenAPI validation
- [ ] Missing DB indexes review + slow-query logging
- [ ] Request IDs on all responses + metrics endpoint
- [ ] Production Docker config + Compose stack
- [ ] PostgreSQL backup scripts
- [ ] Environment variable reference doc + HTTPS reverse proxy guidance
- [ ] Job queue abstraction for async notifications + cleanup

---

## Logging

Structured JSON logs:
```
logs/YYYY-MM/YYYY-MM-DD/
├── app.log      # Normal application + request logs
└── errors.log   # Error-level records only
```
Retention: `LOG_RETENTION_DAYS` (default 90 days).

---

## Known API Bugs Fixed

| Bug | Fix |
|---|---|
| Admin nursery creation fails (unique `owner_user_id`) | `OwnerUserID *int64` in DTO; service sets only for non-admin actors |
| Driver tracking `/latest` returns 500 when no location | Return `nil, nil` on `ErrNoRows`; `PointResponse.Tracking` → `*TrackingPoint` |
| `GET /me/workspaces` returns 500 | Owner via `nurseries.owner_user_id`; manager via `nursery_users.status='ACTIVE'` |
| Driver approve returns 404 | `WHERE driver_id=$1` (was incorrectly `WHERE user_id=$1`) |
| Invite accept had no side effects | MANAGER_INVITE → inserts `nursery_users`; NURSERY_ONBOARDING_INVITE → grants NURSERY_OWNER role |
| Missing quotation endpoints | Added `POST /quotations/:id/approve` and `POST /quotations/:id/convert-to-order` |
