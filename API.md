# GreenRoot — API Reference

> Last updated: 2026-07-19

---

## Stack

Go 1.26.10 · chi router · PostgreSQL + PostGIS · Redis (optional) · JWT/OTP auth (mobile + `123456` mock OTP in dev)

Run locally (port 8080):
```bash
cd greenroot-api
# fresh DB: ../greenroot-infra/scripts/reset-dbs.sh (schema.sql is the schema; no migrations before launch)
DATABASE_URL='postgres:///greenroot?host=/tmp' JWT_SECRET='local-dev-change-me' LOG_DIR='/tmp/gr-logs' go run ./cmd/api

# With Redis rate-limiting (optional):
REDIS_ADDR='localhost:6379' DATABASE_URL='postgres:///greenroot?host=/tmp' JWT_SECRET='local-dev-change-me' LOG_DIR='/tmp/gr-logs' go run ./cmd/api
```

Swagger: `http://localhost:8080/swagger/`  
OpenAPI spec (source of truth): `docs/swagger/openapi.yaml` — every registered route is documented; `internal/app/openapi_test.go` fails CI if a route is added/removed without updating it or a `$ref` breaks.

**CORS:** `CORS_ALLOWED_ORIGINS` is a comma-separated list of exact origins (`https://admin.example.com`, no path). Local dev servers are allowed by default only when `APP_ENV` is not `production`; in production the list must be set or browser clients are refused (startup warns). No credentials (bearer tokens only); `Content-Disposition`, `X-Subscription-Warning` and `X-Request-Id` are exposed to browser JS; preflights are cached 10 minutes. Native mobile apps send no `Origin` and are unaffected.

**Client IP:** `internal/common/clientip` resolves it once per request; everything (rate limits, sessions, audit, document-verify logs, request logs) reads `clientip.From(r)` — never `r.RemoteAddr` or headers. `X-Forwarded-For` / `X-Real-IP` count only when the TCP peer is in `TRUSTED_PROXIES` (CIDRs/IPs); the chain is walked right-to-left to the first untrusted hop. Empty (default) = the peer address, port stripped. A malformed value stops startup in every environment. **Behind a load balancer, set it** — otherwise every client shares the balancer's rate-limit bucket.

Current registered route count from chi route definitions:

| Scope | Count |
|---|---:|
| `/api/v1` module routes | 232 |
| Health routes | 3 |
| Docs/Swagger routes | 4 |
| **Total registered APIs** | **231** |

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
├── policy.go        # optional — role/permission predicates (orders, dispatches, quotations, subscriptions)
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
GET  /api/v1/auth/me
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
| `NURSERY_OWNER` | Owns one nursery; manages top items, orders, managers, customers |
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
| Top items (showcase, max 20) | read | global | own nursery | own nursery (edit) | read | read |
| Orders | — | global | own nursery | own nursery | — | own orders |
| Payments | — | global | order-linked | — | — | own |
| Subscriptions | plans | global | own | — | — | — |
| Vehicles | — | global write; owners; approve lorries and driver links | — | — | own lorry links (`/drivers/me/vehicles`) | — |
| Drivers | — | global write | — | — | own profile | — |
| Dispatches | — | global | own nursery | own nursery | assigned | own orders |
| Tracking | — | global (vehicle history: admin only) | connected drivers + own nursery's dispatches | same as owner | own history + assigned dispatches | own orders' dispatches |
| Notifications | — | global/templates | own | own | own | own |
| Audit | — | ✅ | — | — | — | — |
| Admin | — | ✅ | — | — | — | — |

**Authorization lives in the service layer.** Handlers only extract actor context.

**Tracking history** (`GET /vehicles|drivers|dispatches/{id}/tracking[/latest]`, fixed 2026-10-17 — before this any signed-in user could read any GPS history): a forbidden caller gets `403` whether or not the record exists. Rules live in `tracking/service.go` `canView` + `tracking/repository.go` `CanViewDriver` / `CanViewDispatch` and mirror the dispatches module (owner or active `nursery_users` member; driver connection `CONNECTED`/`APPROVED`; buyer via `orders.buyer_user_id`).

### Lifecycle Capabilities

Orders, dispatches, quotations, and subscriptions return a computed `capabilities` object alongside the resource. It tells the client which actions the current actor may take (`can_confirm`, `can_cancel`, `can_start_loading`, etc.) so UI action buttons are backend-driven, not hardcoded. Mobile and admin consume this directly — never re-derive permissions in the frontend.

---

## Business Rules (API Enforced)

See `BUSINESS_RULES.md` for the full rule set. Key enforcements:

- **One owner → one nursery.** Shared/partner ownership not supported.
- **Manager exclusivity.** MANAGER_INVITE rejected if user owns nursery. NURSERY_ONBOARDING_INVITE rejected if user is manager. `uq_manager_one_active_nursery` DB constraint enforces single active nursery per user.
- **Driver independence.** Drivers join individual trips via UUID/QR; no nursery ownership.
- **Vehicle ownership (2026-10-30, `20261030_01`).** A lorry's owner is a `vehicle_owners` record and needs no GreenRoot account (`user_id` only when the owner also uses the app; an owner who drives uses one normal driver account). One owner has many lorries; a lorry has many drivers and a driver may drive lorries of different owners through `vehicle_drivers` links (`PENDING → APPROVED/REJECTED`, `APPROVED ↔ SUSPENDED`, `→ REMOVED`), each approved by GreenRoot. `vehicles.approval_status` is GreenRoot's check of the lorry itself. Approving a driver also approves the lorry link (and still-pending lorry) in their application. The licence stays on `drivers`; registration, type, capacity and owner stay on `vehicles`.
- **Taking a trip with a lorry.** `POST /dispatches/{id}/accept` takes `vehicle_id` (optional when only one of the driver's lorries can take the trip). The lorry must be linked to the driver and APPROVED, the lorry APPROVED and ACTIVE (not MAINTENANCE), not on another active trip, of the `required_vehicle_type`, and the driver's licence not expired. `public.driver_vehicle_eligibility` (a view) holds every rule but the trip type and gives `blocked_reason`; the claim UPDATE re-checks it, so one lorry is never on two trips. The driver's QR preview carries `driver_vehicles` with `is_eligible`, `blocked_reason` and `blocked_message`. Errors: 409 `no_eligible_vehicle`, `vehicle_not_eligible`, `vehicle_on_active_trip`, `vehicle_type_mismatch`; 422 `vehicle_required`; 403 `vehicle_not_yours`. Two drivers scanning one QR at once: the second waits for the lock and gets `trip_taken`.
- **Nothing is unlinked or parked mid-trip.** While a lorry is on an active trip: the driver cannot remove its link (`DELETE /drivers/me/vehicles/{linkId}`), admins cannot remove the link, set the lorry to MAINTENANCE/INACTIVE/RETIRED, or retire it (409 `vehicle_on_active_trip`). Suspending a link or a lorry is allowed: it stops future trips only. Retiring a lorry ends its open links.
- **Image uploads (2026-10-30, `20261030_02`).** Photos go through `POST /storage/images` (multipart `category` then `file`), never presign; the API checks each against its category's policy in `image_policies` (`internal/common/imagepolicy`: real type from bytes, full decode, 40 MP header cap before decoding, longest/shortest side, max bytes, allowed formats) and stores it in the category's bucket. Selfies (`POST /users/me/avatar`) use the SELFIE policy. `GET /image-policies` lets the apps compress to the same values. Admins read `GET /admin/image-policies` (with defaults and bounds); only SUPER_ADMIN may `PUT /admin/image-policies/{category}` (within bounds, `change_note` required, `version` for optimistic locking) — each save is a new version in `image_policy_versions` and the audit log (module STORAGE). Market ads accept only photos stored in `market-ads`. Presign takes documents only (`image/*` → 400 `use_image_upload`).
- **Plant names (2026-10-30, `20261030_03`).** `public.plant_search_key()` is the one normal form for matching (case, punctuation, spacing; any script). `plant_aliases` holds other names of catalog plants (LOCAL, COMMON, SYNONYM, TRADE, MISSPELLING, with language/region); trigram GIN indexes on aliases, plant names and suggestions. `GET /plants/lookup` returns catalog plants by name, then alias (`match_type` ALIAS + `matched_name`), then near spellings (FUZZY), then WFO names by substring (BOTANICAL) and near spelling (BOTANICAL_FUZZY, 4+ letters; ~25–200 ms on 1.66 M rows). Nothing is picked for the user. `plant_suggestions` is the registry of names people could not find (`source` SUGGESTION or QUOTATION_ITEM, `usage_count`, similar open names); review LINKED makes the name an alias (re-linking retires the old one), MERGED folds a duplicate into another name. Admin: `GET/POST /plants/{id}/aliases`, `GET /plants/aliases`, `PUT /plants/aliases/{aliasId}` (retire with `is_active` false).
- **Customer quotation requests (2026-10-30, `20261030_04`).** See `greenroot-infra/docs/quotations.md` → Customer requests. Routes: `POST /quotations/requests`, `POST /quotations/{id}/price`, `POST /quotations/{id}/decline`, `POST /quotations/{id}/cancel-request`; `send` and `recall` handle request-origin quotations. Order items read with a LEFT JOIN so custom lines (no `plant_id`) show; dispatch item names fall back to the snapshot.
- **Confirmations (2026-10-30, `20261030_05`).** `internal/common/consent`: versioned wording per step; `consent_records` append-only (trigger `forbid_change`). Required (after authorization and state checks): `POST /orders/{id}/complete-loading` (SELLER_LOADING), driver start of a trip with `requires_consent` (DRIVER_DISPATCH; trips before the migration are exempt), `POST /quotations/{id}/buyer-accept` (BUYER_QUOTATION_ACCEPT), `POST /dispatches/{id}/receipt` without an issue (BUYER_DELIVERY). Body `{"consent": {"consent_version", "accepted": true, "evidence"?}}`; 422 `consent_required`, 409 `consent_outdated`. `GET /consents/templates`, `GET /orders/{id}/consents`. See `greenroot-docs/02-product/trust-consent-and-risk.md`.
- **Risk (2026-10-30).** `internal/modules/risk`: detector every 10 min (`risk_rules` thresholds, `risk_signals` one open per finding, `risk_signal_notes` append-only); admin `GET /admin/risk-signals`, `POST /admin/risk-signals/scan`, `GET /admin/risk-signals/{id}`, `POST /admin/risk-signals/{id}/actions`, `GET /admin/risk-rules`, `PUT /admin/risk-rules/{code}` (super admin). A HOLD makes `POST /dispatches` answer 409 `order_on_hold`.
- **`dispatch_assignments` is unused.** No API code reads or writes it (checked 2026-10-30); left in place, not part of the trip flow.
- **Order state machine.** `PENDING → CONFIRMED → LOADING → LOADED/PARTIALLY_FULFILLED → COMPLETED`. Cancel goes via `POST /orders/:id/cancel` (not a status update). Invalid direct transitions are blocked.
- **Order item editing window.** Items (add/edit/remove) allowed in `PENDING`, `CONFIRMED`, `LOADING`. Locked at `LOADED`, `PARTIALLY_FULFILLED`, `COMPLETED`.
- **Loaded quantities.** `PUT /orders/:id/items/:itemId/loaded-quantity` sets actual loaded amount during `LOADING`. If any item is under-loaded at `complete-loading`, order moves to `PARTIALLY_FULFILLED` and invoice is recalculated.
- **Loading declaration.** `complete-loading` needs `{"declared_no_prohibited_items": true}` (else 400 `loading_declaration_required`); recorded in the audit log. Up to 3 loading photos are attachments with `entity_type=ORDER_LOADING`; `GET /orders/:id/loading-proof` shows them (signed links), the declaration and the truck to anyone who can see the order.
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
| users | `GET/PUT/DELETE /users/me` · `POST /users/me/onboarding` · `POST/DELETE /users/me/avatar` (camera selfie; `PUT /users/me` never changes it) · `GET/PUT /users/:id` · addresses, roles, sessions |
| plants | `GET/POST /plants` · `GET/PUT/DELETE /plants/:id` · images, care-guide, categories · `GET /plants/lookup` (picker: catalog + 1.66 M WFO botanical names) · `POST /plants/resolve` (botanical name → catalog plant, added on first use) · `POST /plants/suggestions` (any user: suggest a missing plant → SUBMITTED / ALREADY_SUGGESTED / EXISTS, 10 a day) · admin: `GET /plants/suggestions?status=`, `POST /plants/suggestions/{id}/review` (APPROVED / LINKED with `plant_id` — fills an empty common name — or REJECTED with `note`; notifies the user) · admin: `GET /botanical-names` (browse all names), `/botanical-names/families`, `/botanical-names/{wfoId}` — see `greenroot-docs/03-business-rules/plants.md` |
| nurseries | `GET/POST /nurseries` · `GET/PUT/DELETE /nurseries/:id` · addresses, users |
| topitems | `GET/POST /top-items` · `GET/PUT/DELETE /top-items/:id` · by nursery, by plant — a nursery's showcase (max 20; plant, size, optional price, notes, photos). Not stock. |
| orders | `GET/POST /orders` · `GET/DELETE /orders/:id` · status, items, loaded-quantity, start-loading, complete-loading, cancel, assign-manager |
| payments | `GET/POST /payments` · manual, status · by order, by subscription |
| subscriptions | `GET /subscription-plans` · `GET/POST /subscriptions` · renew, cancel, status |
| vehicles | `GET/POST /vehicles` · `GET/PUT/DELETE /vehicles/:id` · `PUT /vehicles/:id/approval` · `GET/POST /vehicle-owners` · `GET/PUT /vehicle-owners/:id` · `GET /vehicle-drivers` · `PUT /vehicle-drivers/:id/status` (admin) |
| drivers | `GET/POST /drivers` · `GET/PUT/DELETE /drivers/:id` · location · `GET/POST /drivers/me/vehicles` · `DELETE /drivers/me/vehicles/:linkId` |
| dispatches | `GET/POST /dispatches` · `GET /dispatches/:id` · status, items · by order |
| tracking | `POST /tracking` · vehicle/driver/dispatch latest + history |
| notifications | `GET/POST /notifications` (list carries `unread_count`, the caller's own total) · read, read-all, delete, clear read (`DELETE /notifications/read`) · devices, templates · daily cleanup of notifications read > 90 days ago (unread never) |
| attachments | `GET/POST /attachments` · `GET/DELETE /attachments/:id` |
| audit | `GET /audit-logs` |
| admin | `GET /admin/dashboard` (real metrics, business-day "today", 7-day `trend`) · `GET /admin/users` · `GET /admin/crash-reports` (with `user_name`) |
| health | `GET /health` · `/healthz` · `/readyz` |
| quotations | `POST /quotations/:id/send` · `POST /quotations/:id/convert-to-order` |
| invites | `POST /invites` · `GET /invites/:uuid` · accept, cancel · nursery invites + managers |
| me/workspaces | `GET /me/workspaces` — returns PERSONAL, OWNED_NURSERY, MANAGER_NURSERY, DRIVER workspaces |
| market | `GET/POST /market/ads` · browse, mine, saved, publish/pause/resume/renew/archive · enquiries · admin moderation: `GET /market/reports`, `POST /market/reports/{id}/review` (`REVIEWED`/`DISMISSED`, optional `archive_ad`) |
| ratings | `GET /ratings` · `POST /ratings/app` · order + trip submit and fetch |
| reports | `GET /reports/{type}?from&to` (type: orders, quotations, deliveries; optional `format=csv`, `view=buying`, admin `nursery_id`) — summary + paged rows or streamed CSV. Owner: own nursery · manager: assigned to / created by them · driver: own trips (deliveries only) · customer: own purchases · admin: all. India days, max 3 years, mobiles masked, CSV formula-safe. Sales = COMPLETED orders; paid = SUCCESS payments; drafts left out |
| email | `GET /email/status` · sender, templates (+ preview, test send), messages · `POST /users/me/email/send-code`, `/users/me/email/verify` — see [`greenroot-infra/docs/messaging.md`](../greenroot-infra/docs/messaging.md) |
| messaging (core) | No routes. `messaging.Notifier` fans business events (payments, welcome) out to the SMS and email channels; `internal/common/otp` is shared by login OTP and email verification |
| sms | `GET /sms/status` · entity, headers, templates (+ preview, test send), messages · `GET/PUT /sms/preferences/me` — TRAI DLT registry; see [`greenroot-infra/docs/sms.md`](../greenroot-infra/docs/sms.md) |

---

## Registered API List

All module routes below are mounted under `/api/v1` unless noted.

| Module | Count | Registered routes |
|---|---:|---|
| Health | 3 | `GET /health`, `GET /healthz`, `GET /readyz` |
| Docs/Swagger | 4 | `GET /openapi.yaml`, `GET /swagger`, `GET /swagger/`, `GET /swagger/index.html` |
| Auth | 7 | `POST /api/v1/auth/send-otp`, `POST /api/v1/auth/verify-otp`, `POST /api/v1/auth/refresh-token`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/me`, `GET /api/v1/me/workspaces`, `GET /api/v1/me/owner-dashboard` |
| Admin | 2 | `GET /api/v1/admin/dashboard`, `GET /api/v1/admin/users` |
| Users | 13 | `GET /api/v1/users/me`, `PUT /api/v1/users/me`, `DELETE /api/v1/users/me`, `POST /api/v1/users/me/onboarding`, `POST /api/v1/users/me/avatar`, `DELETE /api/v1/users/me/avatar`, `GET /api/v1/users/{id}`, `GET /api/v1/users/{id}/addresses`, `POST /api/v1/users/{id}/addresses`, `PUT /api/v1/users/addresses/{addressId}`, `DELETE /api/v1/users/addresses/{addressId}`, `GET /api/v1/users/{id}/roles`, `GET /api/v1/users/{id}/sessions` |
| Plants | 12 | `GET /api/v1/plants`, `POST /api/v1/plants`, `GET /api/v1/plants/sizes`, `GET /api/v1/plants/categories`, `POST /api/v1/plants/categories`, `PUT /api/v1/plants/categories/{categoryId}`, `DELETE /api/v1/plants/categories/{categoryId}`, `GET /api/v1/plants/{id}`, `PUT /api/v1/plants/{id}`, `DELETE /api/v1/plants/{id}`, `POST /api/v1/plants/{id}/images`, `GET /api/v1/plants/{id}/care-guide` |
| Nurseries | 18 | `GET /api/v1/nurseries`, `POST /api/v1/nurseries`, `GET /api/v1/nurseries/mine`, `GET /api/v1/nurseries/owned`, `GET /api/v1/nurseries/{id}`, `PUT /api/v1/nurseries/{id}`, `PUT /api/v1/nurseries/{id}/status`, `DELETE /api/v1/nurseries/{id}`, `GET /api/v1/nurseries/{id}/addresses`, `POST /api/v1/nurseries/{id}/addresses`, `PUT /api/v1/nurseries/addresses/{addressId}`, `DELETE /api/v1/nurseries/addresses/{addressId}`, `GET /api/v1/nurseries/{id}/managers`, `DELETE /api/v1/nurseries/{id}/managers/{userId}`, `GET /api/v1/nurseries/{id}/drivers`, `POST /api/v1/nurseries/{id}/drivers`, `POST /api/v1/nurseries/{id}/drivers/{driverUserId}/approve`, `GET /api/v1/nurseries/{id}/customers` |
| Top Items | 7 | `GET /api/v1/top-items`, `POST /api/v1/top-items`, `GET /api/v1/top-items/{id}`, `PUT /api/v1/top-items/{id}`, `DELETE /api/v1/top-items/{id}`, `GET /api/v1/nurseries/{nurseryId}/top-items`, `GET /api/v1/plants/{plantId}/top-items` |
| Orders | 16 | `GET /api/v1/orders`, `POST /api/v1/orders`, `GET /api/v1/orders/{id}`, `PUT /api/v1/orders/{id}/status`, `PUT /api/v1/orders/{id}/delivery`, `DELETE /api/v1/orders/{id}`, `POST /api/v1/orders/{id}/confirm`, `POST /api/v1/orders/{id}/start-loading`, `POST /api/v1/orders/{id}/complete-loading`, `POST /api/v1/orders/{id}/cancel`, `POST /api/v1/orders/{id}/assign-manager`, `GET /api/v1/orders/{id}/items`, `POST /api/v1/orders/{id}/items`, `PUT /api/v1/orders/{id}/items/{itemId}`, `DELETE /api/v1/orders/{id}/items/{itemId}`, `PUT /api/v1/orders/{id}/items/{itemId}/loaded-quantity` |
| Quotations | 23 | `GET /api/v1/quotations`, `POST /api/v1/quotations`, `GET /api/v1/quotations/by-token/{token}`, `GET /api/v1/quotations/{id}`, `PUT /api/v1/quotations/{id}`, `PUT /api/v1/quotations/{id}/customer`, `DELETE /api/v1/quotations/{id}`, `POST /api/v1/quotations/{id}/assign-manager`, `DELETE /api/v1/quotations/{id}/assign-manager`, `POST /api/v1/quotations/{id}/send`, `POST /api/v1/quotations/{id}/approve`, `POST /api/v1/quotations/{id}/recall`, `POST /api/v1/quotations/{id}/convert-to-order`, `POST /api/v1/quotations/{id}/buyer-accept`, `POST /api/v1/quotations/{id}/buyer-reject`, `POST /api/v1/quotations/{id}/record-download`, `POST /api/v1/quotations/{id}/documents`, `GET /api/v1/quotations/{id}/documents`, `GET /api/v1/quotations/{id}/documents/current`, `GET /api/v1/quotations/{id}/documents/render`, `POST /api/v1/quotations/{id}/verify-token`, `POST /api/v1/quotations/{id}/verify-token/revoke`, `GET /api/v1/verify/{token}` |
| Payments | 6 | `GET /api/v1/payments`, `POST /api/v1/payments/manual`, `GET /api/v1/payments/{id}`, `PUT /api/v1/payments/{id}/status`, `GET /api/v1/orders/{orderId}/payments`, `GET /api/v1/subscriptions/{subscriptionId}/payments` |
| Subscriptions | 16 | `GET /api/v1/subscription-plans`, `GET /api/v1/subscription-plans/{id}`, `PUT /api/v1/subscription-plans/{id}`, `GET /api/v1/promos`, `POST /api/v1/promos`, `PUT /api/v1/promos/{id}`, `POST /api/v1/promos/validate`, `POST /api/v1/promos/{id}/blast`, `GET /api/v1/subscriptions`, `POST /api/v1/subscriptions`, `GET /api/v1/subscriptions/me`, `GET /api/v1/subscriptions/{id}`, `PUT /api/v1/subscriptions/{id}/status`, `POST /api/v1/subscriptions/{id}/renew`, `POST /api/v1/subscriptions/{id}/cancel`, `GET /api/v1/subscriptions/{id}/payments` |
| Dispatches | 13 | `GET /api/v1/track/{uuid}`, `GET /api/v1/dispatches`, `POST /api/v1/dispatches`, `GET /api/v1/dispatches/code/{code}` (trip code from the QR, never the dispatch code), `GET /api/v1/dispatches/{id}`, `PUT /api/v1/dispatches/{id}/status`, `POST /api/v1/dispatches/{id}/ack-delivery-update`, `POST /api/v1/dispatches/{id}/deliver` (customer's code or staff override), `POST /api/v1/dispatches/{id}/receipt` (customer), `POST /api/v1/dispatches/{id}/accept` (needs `trip_code`), `POST /api/v1/dispatches/{id}/items`, `POST /api/v1/dispatches/{id}/trip-events`, `GET /api/v1/orders/{orderId}/dispatches` |
| Drivers | 13 | `POST /api/v1/drivers/apply`, `GET /api/v1/drivers/me`, `DELETE /api/v1/drivers/me` (stop driving), `GET /api/v1/drivers/me/vehicles`, `POST /api/v1/drivers/me/vehicles`, `DELETE /api/v1/drivers/me/vehicles/{linkId}`, `GET /api/v1/drivers`, `POST /api/v1/drivers`, `GET /api/v1/drivers/{id}`, `PUT /api/v1/drivers/{id}`, `DELETE /api/v1/drivers/{id}`, `POST /api/v1/drivers/{id}/approve`, `POST /api/v1/drivers/{id}/location` |
| Vehicles | 12 | `GET /api/v1/vehicles`, `POST /api/v1/vehicles`, `GET /api/v1/vehicles/{id}`, `PUT /api/v1/vehicles/{id}`, `DELETE /api/v1/vehicles/{id}`, `PUT /api/v1/vehicles/{id}/approval`, `GET /api/v1/vehicle-owners`, `POST /api/v1/vehicle-owners`, `GET /api/v1/vehicle-owners/{id}`, `PUT /api/v1/vehicle-owners/{id}`, `GET /api/v1/vehicle-drivers`, `PUT /api/v1/vehicle-drivers/{id}/status` |
| Tracking | 10 | `POST /api/v1/tracking`, `POST /api/v1/tracking/live`, `GET /api/v1/tracking/live/drivers/{driverUserId}`, `GET /api/v1/tracking/live/nearby`, `GET /api/v1/dispatches/{dispatchId}/tracking`, `GET /api/v1/dispatches/{dispatchId}/tracking/latest`, `GET /api/v1/drivers/{driverId}/tracking`, `GET /api/v1/drivers/{driverId}/tracking/latest`, `GET /api/v1/vehicles/{vehicleId}/tracking`, `GET /api/v1/vehicles/{vehicleId}/tracking/latest` |
| Notifications | 14 | `GET /api/v1/notifications`, `POST /api/v1/notifications`, `PUT /api/v1/notifications/read-all`, `DELETE /api/v1/notifications/read` (caller's read ones), `GET /api/v1/notifications/devices`, `POST /api/v1/notifications/devices`, `DELETE /api/v1/notifications/devices/{id}`, `GET /api/v1/notifications/templates`, `POST /api/v1/notifications/templates`, `PUT /api/v1/notifications/templates/{id}`, `DELETE /api/v1/notifications/templates/{id}`, `GET /api/v1/notifications/{id}`, `PUT /api/v1/notifications/{id}/read`, `DELETE /api/v1/notifications/{id}` |
| Attachments | 4 | `GET /api/v1/attachments`, `POST /api/v1/attachments`, `GET /api/v1/attachments/{id}`, `DELETE /api/v1/attachments/{id}` |
| Invites | 5 | `POST /api/v1/invites`, `GET /api/v1/invites/{uuid}`, `POST /api/v1/invites/{uuid}/accept`, `POST /api/v1/invites/{uuid}/cancel`, `GET /api/v1/nurseries/{nurseryId}/invites` |
| Sourcing | 5 | `GET /api/v1/nurseries/{nurseryId}/sourcing-membership`, `POST /api/v1/nurseries/{nurseryId}/sourcing-membership`, `DELETE /api/v1/nurseries/{nurseryId}/sourcing-membership`, `GET /api/v1/sourcing-network/nurseries`, `GET /api/v1/sourcing-network/nurseries/{nurseryId}` (Need & Availability posts were replaced by Local Market wanted posts, 2026-10-20) |
| Market | 28 | `GET /api/v1/market/nurseries/{id}` (seller profile), admin: `GET /api/v1/market/admin/ads`, `POST /api/v1/market/admin/ads/{id}/moderate`, `POST /api/v1/market/admin/nurseries/{id}/unblock`, `GET /api/v1/market/admin/blocked-nurseries`, `GET /api/v1/market/ads`, `GET /api/v1/market/ads/mine`, `GET /api/v1/market/ads/saved`, `POST /api/v1/market/ads`, `GET /api/v1/market/ads/{id}`, `PATCH /api/v1/market/ads/{id}`, `DELETE /api/v1/market/ads/{id}` (owner soft delete: drafts, or live/paused ads with no chats), `POST /api/v1/market/ads/{id}/publish`, `pause`, `resume`, `renew`, `archive`, `save`, `report` · `POST /api/v1/market/ads/{id}/enquiries`, `GET /api/v1/market/enquiries`, `GET /api/v1/market/enquiries/{id}`, `POST /api/v1/market/enquiries/{id}/reply`, `close`, `cancel`, `offers`, `offers/{messageId}/accept`, `decline` |
| Ratings | 6 | `GET /api/v1/ratings`, `POST /api/v1/ratings/app`, `POST /api/v1/ratings/order/{order_id}`, `GET /api/v1/ratings/order/{order_id}`, `POST /api/v1/ratings/trip/{dispatch_id}`, `GET /api/v1/ratings/trip/{dispatch_id}` |
| Reports | 1 | `GET /api/v1/reports/{type}` |
| SMS | 15 | `GET /api/v1/sms/status`, `GET /api/v1/sms/entity`, `PUT /api/v1/sms/entity`, `GET /api/v1/sms/headers`, `POST /api/v1/sms/headers`, `PUT /api/v1/sms/headers/{id}`, `GET /api/v1/sms/templates`, `POST /api/v1/sms/templates`, `POST /api/v1/sms/templates/preview`, `GET /api/v1/sms/templates/{id}`, `PUT /api/v1/sms/templates/{id}`, `POST /api/v1/sms/templates/{id}/test`, `GET /api/v1/sms/messages`, `GET /api/v1/sms/preferences/me`, `PUT /api/v1/sms/preferences/me` |
| Email | 12 | `GET /api/v1/email/status`, `GET /api/v1/email/sender`, `PUT /api/v1/email/sender`, `GET /api/v1/email/templates`, `POST /api/v1/email/templates`, `POST /api/v1/email/templates/preview`, `GET /api/v1/email/templates/{id}`, `PUT /api/v1/email/templates/{id}`, `POST /api/v1/email/templates/{id}/test`, `GET /api/v1/email/messages`, `POST /api/v1/users/me/email/send-code`, `POST /api/v1/users/me/email/verify` |
| Storage | 6 | `POST /api/v1/storage/presign` (documents), `POST /api/v1/storage/images` (photos, policy-checked), `GET /api/v1/image-policies`, `GET /api/v1/admin/image-policies`, `PUT /api/v1/admin/image-policies/{category}` (super admin), `GET /api/v1/admin/image-policies/{category}/history` |
| Audit | 1 | `GET /api/v1/audit-logs` |

---

## Provider Status (Intentionally Mocked for V1)

| Provider | Status |
|---|---|
| FCM (Push notifications) | Mock — real credentials needed |
| S3 File Storage | Mock — pre-signed URL logic pending |
| Razorpay Payments (subscriptions) | **Live when `RAZORPAY_KEY_ID` + `RAZORPAY_KEY_SECRET` are set** (Standard Checkout: `POST /subscriptions/payment-order` → app checkout → create/renew verifies signature + order before extending); mock otherwise. Admin refunds: `POST /subscriptions/{id}/payments/{paymentID}/refund`. Webhook not built. See `greenroot-infra/docs/subscriptions.md` §3 |
| Email Provider | Mock (`EMAIL_PROVIDER=mock`): verification code `123456`, emails logged as MOCKED. Live path built (random hashed code, verified sender/domain required) — **Amazon SES built, off until enabled** (`EMAIL_PROVIDER=ses` + `AWS_REGION`). See `greenroot-infra/docs/messaging.md` |
| SMS / OTP Provider | Mock (`SMS_PROVIDER=mock`): `123456` OTP, SMS logged as MOCKED. Live path built (random hashed OTP, per-number throttle, DLT-approved templates only) — **AWS End User Messaging SMS built, off until enabled** (`SMS_PROVIDER=aws` + `AWS_REGION`, after DLT). See `greenroot-infra/docs/sms.md` |

---

## Production safety & migrations

- **Startup validation** (`internal/common/config/validate.go`): with `APP_ENV=production` the API refuses to start, listing every problem, if any of these hold:
  - `JWT_SECRET` is the default or shorter than 32 bytes;
  - `DATABASE_URL`, `REDIS_ADDR` or `CORS_ALLOWED_ORIGINS` is missing;
  - storage keys are the MinIO defaults;
  - SMS or email is mock — unless `ALLOW_MOCK_PROVIDERS=true`, which is for a closed beta only.
- **Body limits:** 1 MB for JSON (`HTTP_MAX_BODY_BYTES`), 12 MB for multipart uploads (`HTTP_MAX_UPLOAD_BYTES`). A declared oversize body gets `413 payload_too_large`.
- **Database roles (D9):** `greenroot-infra/db/postgresql/roles.sql` creates `greenroot_owner` (owns the schema, no login), `greenroot_migrator` (runs `cmd/migrate` via `MIGRATE_DATABASE_URL`), `greenroot_app` (the API's `DATABASE_URL`: row reads/writes only, 30 s statement timeout, cannot touch `schema_migrations`) and `greenroot_readonly`. At startup the API checks its own role (`database.PrivilegeProblems`): superuser, `BYPASSRLS`, schema owner or `CREATE` on `public` → production refuses to start, other environments warn. CI runs the integration suite this way.
- **Database-enforced rules (2026-10-17, `20261017_01`):** 27 `ck_*` constraints — every lifecycle status column (orders, payments, dispatches, subscriptions, nurseries, users, sessions, drivers, vehicles, invites, notification channel) is limited to the values its Go validator allows, and amounts/quantities/stock are non-negative; a payment belongs to exactly one order **or** subscription. **Adding a status value now needs a migration.** `uq_payments_provider_payment` stops a gateway payment being recorded twice. `internal/database/pgerr` maps violations to `ErrConflict` (409) / `ErrInvalidInput` (400); payments uses it (`409 duplicate_provider_payment`).
- **Optimistic concurrency (D5):** `version` on orders, quotations, payments, dispatches and top items (trigger-bumped on every update) is returned in every response. Clients may send `If-Match: <version>` on the 9 write endpoints documented in OpenAPI (`#/components/parameters/IfMatch`); a stale value is **409 `version_conflict`** and writes nothing, a malformed one is 400 `invalid_if_match`, no header = unconditional. Independently, every order/dispatch/payment status write is guarded server-side with `WHERE version = <version just read>`, so two racing requests can never both pass a state-machine check (tested: `TestOrderRowVersion`, `TestUpdateStatus_ConcurrentChangeConflicts`). `internal/common/rowversion` parses the header.
- **Time (D1, `20261017_04`):** every timestamp column is `timestamptz` (193 converted, `USING col AT TIME ZONE 'UTC'` — existing API responses byte-identical, verified on 56 endpoint×role pairs). Before, Go-written and `DEFAULT`-written timestamps disagreed by 5.5 h when the server zone was IST. The API pins the session zone with `DB_TIMEZONE` (default `Asia/Kolkata`: business dates for `CURRENT_DATE` and date-based codes) and decodes timestamptz as UTC, so JSON stays `...Z`.
- **Money (D2, `20261017_05`):** every amount is `numeric(15,2)` (quotations were `12,2`); orders, quotations, payments and plans carry `currency` (`INR`, CHECK-pinned). Go still uses `float64` for amounts — see Pre-Production backlog.
- **Naming (D13, `20261017_08`/`_09`):** database names follow one convention (main lifecycle column `status`, timestamps `_at`, booleans `is_…`, actors `<verb>_by_user_id`, keys `<entity>_id`, `ck_`/`idx_`/`uq_` names). **JSON fields and query parameters did not change** — e.g. the API still returns `order_status` and `order_date`, read from `orders.status` and `orders.ordered_at`. Run `DATABASE_URL=... go run ./cmd/sqlcheck ./internal/...` after any schema or SQL change.
- **Migrations:** `internal/database/migrations` is the only place schema changes live. Run `go run ./cmd/migrate status|up|baseline` (or `make migrate-up`). In production the API refuses to start while migrations are pending. Details: `internal/database/migrations/README.md`.
- **CI** (`.github/workflows/ci.yml`, on `master` and PRs). Three jobs, all must pass:
  - **unit:** gofmt, vet, `govulncheck` (no reachable vulnerabilities), `gosec` (every exception is an inline `#nosec <rule> -- <reason>`), `go test -race` (excluding `./tests/...`), builds of all four commands. The OpenAPI drift test runs as part of `internal/app`.
  - **integration:** PostGIS 17 + Redis 7 services; `cmd/sqlcheck` PREPAREs every SQL statement in the code (415) against the migrated schema, so a wrong column/table fails CI; `tests/api/naming_test.go` enforces the schema naming convention (D13); loads `greenroot-infra` `schema.sql` + `seed-master.sql` + `seed-dev-admin.sql`, runs `cmd/migrate up`, starts the API, runs `go test ./tests/...`.
  - **secrets:** `gitleaks` over the full git history.
- **Dependabot** (`.github/dependabot.yml`): weekly Go module updates, monthly Actions updates.
- **Pull requests** use `.github/pull_request_template.md` (Definition of Done from `greenroot-docs/06-decisions/enterprise-engineering-standards.md`); `CODEOWNERS` covers migrations, `internal/common`, `internal/app`, swagger and `.github`.
- **Accepted risk:** `golang.org/x/crypto` GO-2026-5932 has no fixed release and is not reachable from our code (govulncheck confirms).

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

API integration tests (`tests/api`, run in CI): start the API on `:8080`, then `go test -count=1 ./tests/...`. They seed their own fixtures and need only `schema.sql` + `seed-master.sql` + `seed-dev-admin.sql` (not the API dev-data script — its users clash with the fixtures). Rate-limit buckets (`rl:*`) are cleared before each login by the test helper (all tests log in from 127.0.0.1); `ratelimit_test.go` proves the limits themselves (429 after 10 verifies with forged headers, after 5 sends per mobile). Current result: 72 pass, 3 skip (fixtures; see the standards doc §6). `tests/test-api.sh` was removed (2026-10-19); the journey suite below replaced it.

**User journeys** (`tests/journeys/journeys.py`, Python standard library only, run in CI after `tests/api`): signs up fresh users on random 6/7/8-prefixed mobiles and walks every lifecycle with positive and negative checks — auth (OTP, refresh rotation, logout, expired/forged/revoked tokens), accounts (suspend, delete, re-signup), nursery (register → reject → resubmit → approve → suspend → reactivate, invites, role exclusivity, cross-owner access), quotation, order, dispatch + tracking, payments, market, admin RBAC, Redis side effects (rate limits, workspace cache, search-cache TTLs, locks), role changes on one account (leave / stop driving / close nursery, then another role) and access (subscription and suspension changes applying at once). 1216 checks, about 10 s. Run against a running API: `REDIS_ADDR=localhost:6379 JWT_SECRET=<api secret> python3 -I tests/journeys/journeys.py [--only order,dispatch]`. Works on a fresh `schema.sql` + seeds; the botanical-names load is optional.

---

## Infrastructure — PostGIS & Redis

### PostGIS (migrations 013–014)

Spatial queries and location storage for all modules.

| Table | Column | Notes |
|---|---|---|
| `nursery_addresses` | `location geography(Point,4326)` | Synced from lat/lon by trigger |
| `user_addresses` | `location geography(Point,4326)` | Synced from lat/lon by trigger |
| `driver_locations` | `location geography(Point,4326)` | Synced from lat/lon by trigger |
| `order_delivery_snapshots` | `location geography(Point,4326)` | Written inline by orders repo |
| `market_ads` | `pickup_location geography(Point,4326)` | Synced from pickup_lat/lon by trigger |

GIST indexes on all geography columns. Shared trigger function: `sync_latlong_to_geography()`.

Shared Go helpers: `internal/common/location/geo.go` — `MakePoint`, `DWithin`, `DistanceKM`.

Modules using PostGIS queries:
- **sourcing** — `ListNearby` uses `ST_Distance` on `nursery_addresses.location`
- **market** — `ListPublished` accepts `near_lat`, `near_lon`, `radius_km` query params; filters with `ST_DWithin` on `pickup_location`
- **orders** — `order_delivery_snapshots` written with inline PostGIS expression

### Redis (optional)

Set `REDIS_ADDR` to enable. API degrades gracefully (no-op) when not configured.

| Env var | Default | Notes |
|---|---|---|
| `REDIS_ADDR` | _(empty — Redis disabled)_ | `host:port` e.g. `localhost:6379` |
| `REDIS_PASSWORD` | _(empty)_ | |
| `REDIS_DB` | `0` | |

Current usage:
- **Distributed locks** — state transitions for orders, dispatches, quotations, and subscription activation
- **OTP lifecycle** — 5-minute OTP storage and one-time verification
- **JWT blocklist** — logout/account deletion token invalidation by `jti`
- **Workspace bootstrap cache** — short-lived `/api/v1/workspaces` cache per user
- **Subscription plan cache** — one-hour full plan cache
- **Market counters** — ad views/saves buffered in Redis and flushed to Postgres
- **Notification queue** — Redis Stream-backed async push notification delivery
- **Expiry scheduling** — sorted-set scheduling for quotation and subscription expiry
- **Live driver tracking** — Redis GEO stores only current active-trip driver locations

Middleware: `internal/middleware/ratelimit.go` — `OTPRateLimit`, `VerifyRateLimit`, `RateLimit`.

### Live Driver Tracking: PostGIS vs Redis GEO

PostGIS remains the permanent source of truth. Redis GEO is only a temporary live-location index for active drivers during active trips.

```
PostGIS
---------
Permanent business locations
Historical locations
Nearby market queries
Distance calculations

Redis GEO
----------
Current live driver location only
Temporary
Fast in-memory lookup
Automatically cleaned after trip completion
```

API endpoints:
- `POST /tracking/live` — updates the current driver live location in Redis GEO. Drivers update their own location; admins can provide `driver_user_id`.
- `GET /tracking/live/drivers/{driverUserId}` — returns the latest live Redis GEO location when still fresh.
- `GET /tracking/live/nearby?latitude={lat}&longitude={lon}&radius_km={km}&limit={n}` — returns active live drivers near a coordinate.

Runtime behavior:
- Driver clients should send live updates every 10-20 seconds during an active trip.
- The live `last_seen` key uses a short TTL, currently 90 seconds.
- When a dispatch is `DELIVERED`, `CANCELLED`, or future terminal `EXPIRED`, the service removes that driver's Redis GEO member.
- If Redis is unavailable, live tracking is skipped and logged; normal dispatch/trip operations continue.
- Permanent trip events and historical tracking rows continue to be stored in PostgreSQL/PostGIS.

### New fields on dispatch responses

`delivery_latitude` and `delivery_longitude` are now included in all `Dispatch` responses when the order has a confirmed delivery snapshot. Drivers can use these coordinates to open external navigation apps.

### Market nearby search

`GET /market/ads` now accepts:
- `near_lat` — buyer latitude
- `near_lon` — buyer longitude  
- `radius_km` — search radius (default 50 km when near_lat/near_lon provided)

Only returns ads with a confirmed `pickup_location`. Combines with all existing filters.

`sort`: `newest` (default), `oldest`, `name` (plant name A→Z), `price_asc`, `price_desc`, `popular`, `nearest`.

**The market is local.** Browse always limits to ads within `radius_km` of the viewer: default 60, capped at 100. The position is `near_lat`/`near_lon` (the phone) or, when absent, the viewer's nursery address; with neither the API returns 422 `location_required`. Each ad carries `distance_km`. Admins are not limited. An ad cannot be published without a location (GPS sent while posting, or the nursery's coordinates). All lists page with `page` / `per_page` (max 50) and return `total`.

---

## Pre-Production Hardening Backlog

- [x] Rate limiting on OTP endpoints (Redis)
- [x] Rate limits cannot be bypassed (2026-10-17): client IP from trusted proxies only, no port in the key; send-otp keyed on the body's `mobile` (it read the URL query, so it never applied)
- [ ] Rate limiting on remaining high-risk routes
- [x] Request size limits (2026-10-17: 1 MB JSON / 12 MB multipart, 413)
- [ ] Security headers (HSTS, X-Frame-Options)
- [x] CORS policy review per environment (2026-10-16: exact-origin list, prod-safe defaults, exposed headers, no credentials)
- [ ] Login brute-force protection
- [ ] Real FCM, S3, Razorpay/PayU integrations
- [ ] Expand integration tests — edge cases + negative paths
- [x] CI: gofmt, vet, race tests, builds, OpenAPI drift, govulncheck, gosec, gitleaks, integration job (2026-10-17). CD/deploy still pending.
- [x] Missing DB indexes review (2026-10-16: all 75 unindexed FKs checked against repository SQL; 4 indexes added; now part of `greenroot-infra/db/postgresql/schema.sql`)
- [ ] Slow-query logging (server settings documented in `greenroot-infra/docs/operations/database-operations.md`; enable on the production host)
- [ ] Admin and mobile send `If-Match` with the `version` they loaded (API ready since 2026-10-17)
- [ ] Go money type: replace `float64` amounts with integer paise / decimal (D2)
- [ ] Branch protection on `master` (GitHub settings — owner action)
- [ ] Enterprise roadmap Phases 1–4 (`greenroot-docs/06-decisions/enterprise-engineering-standards.md`): coverage ≥ 70%, version columns, idempotency keys, money type, `timestamptz`, `sqlc`, problem+json, policy layer, RLS, token rotation, OpenTelemetry
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
| Missing quotation endpoints | Added explicit `POST /quotations/:id/send`; `POST /quotations/:id/approve` remains an alias |
| `GET /auth/me` returns 404 | Handler was implemented but never wired into `auth/routes.go`; added `r.Get("/me", m.handler.Me)` |
| `inventory/create` fails on second run | Plain INSERT hit `uq_inventory(nursery_id,plant_id,size_id)`; made `Create` an upsert with `ON CONFLICT DO UPDATE` |
| Tracking history readable by any signed-in user (IDOR, 2026-10-17) | Per-subject access checks in `tracking/service.go`; 403 without leaking existence; authz-matrix unit test |
| `GET /me/owner-dashboard` showed 0 orders/inventory/drivers/customers and a wrong quote breakdown (2026-10-17) | Sell orders by `seller_nursery_id`, inventory by its status column (then `inventory_status`, now `nursery_inventory.status`), drivers from `nursery_drivers`; query errors are returned instead of swallowed; status mapping unit-tested |
| `POST /orders/{id}/cancel` and quotation record-download silently ignored malformed JSON | Body stays optional (empty is fine); malformed JSON now returns `400 invalid_json` |
| Per-IP rate limits bypassable: chi `RealIP` trusted any client's `X-Forwarded-For`, the key included the source port (new connection = new bucket), and `PublicVerify` trusted `X-Real-IP` (2026-10-17) | `internal/common/clientip` + `TRUSTED_PROXIES`; verified live: 10 verify-otp attempts with forged headers, then 429 |
| send-otp limit never applied: read `mobile` from the URL query, body is JSON (2026-10-17) | Reads the JSON body (first 4 KB) and restores it for the handler; verified live: 5 sends, then 429 |
| Expiry worker hot-looped when a quotation/subscription update failed (ids re-read forever) (2026-10-17) | Stops when a batch removes nothing; failed ids retry next tick; regression test proven to fail on old code |
| Order/dispatch/payment status writes raced: the service checked the state machine on a read, then updated unconditionally, so concurrent requests could both pass (e.g. cancel + complete-loading → a LOADED order cancelled) (2026-10-17) | Every such UPDATE is `WHERE version = <read version>`; 0 rows on an existing record → 409 `version_conflict` |
| Timestamps written by `DEFAULT CURRENT_TIMESTAMP` were IST wall clocks read back as UTC (5.5 h off) whenever the DB server zone was not UTC (2026-10-17) | All columns `timestamptz`; session zone pinned; existing responses unchanged |
| `nurseries.UpdateStatus` stored any string as a status (2026-10-17) | Validated against the full status set (also a DB CHECK) |
| Ratings endpoints returned 500 on every fresh install: the `ratings` table was lost when infra migrations were consolidated (2026-10-17, found by `cmd/sqlcheck`) | Migration `20261017_07` restores it; `TestAppRating_UpsertsOnePerUser` |
| Driver was never told about a delivery-address change: `StartedDispatchDriverUserID` joined `nursery_drivers.driver_id`, a column that never existed, and the error was ignored (2026-10-17, found by `cmd/sqlcheck`) | Joins `drivers` on `dispatches.driver_id` |
| `orders.created_by_user_id` was NULL on every row while the creator sat in `created_by`, so the API never returned `created_by_user_id` (2026-10-17) | Dead column dropped, `created_by` renamed into its place, FK to `users` added |
| `quotation_items` had two identical foreign keys on `quotation_id` (2026-10-17, found by the naming test) | Duplicate dropped |
| Any approved owner could edit, re-status, delete, or change addresses of **any** nursery: the check was "has NURSERY_OWNER", not "owns this nursery" (2026-10-19, journeys) | `nurseries.Service.canManage` checks ownership (addresses via their nursery); status is no longer editable through `PUT /nurseries/{id}` |
| Managers could register a nursery: `UserIsManager` read `nursery_role_id = 3`, which invites never set; the duplicate role column also allowed duplicate memberships (2026-10-19) | `nursery_users.role` is the only role (CHECK `MANAGER`), `nursery_role_id` + `nursery_roles` dropped, UNIQUE `(nursery_id, user_id)`; rejoining reactivates the row |
| `POST /nurseries/{id}/managers` made anyone a manager with no invite or consent and skipped the owner/driver exclusivity checks; unused by admin and mobile (2026-10-19) | Route, handler, service and repository code removed (also the unrouted `AddUser`) — managers join only by invite |
| Nursery rejection reason was dropped; `PUT /nurseries/{id}/status` accepted any status, so SUSPENDED there skipped session revocation (2026-10-19) | That route is the review decision only: PENDING → APPROVED/REJECTED (reason required, stored, cleared on approve); 409 `not_pending`; suspend/reactivate via `/admin/nurseries/{id}/status` |
| A manager who left kept the MANAGER role and never became a BUYER again (2026-10-19) | `RemoveUser` (leave and owner removal) drops MANAGER and restores BUYER when no other membership or DRIVER role remains, in one transaction |
| Quotations: unknown `quotation_type` saved as CUSTOMER; manager pre-assign silently dropped; ACCEPTED quotations deletable; owner could not open a manager's draft; customer details and rejection reason reached managers through every response except Get/List; `valid_until` in the past accepted (2026-10-19) | 400 for unknown type; 403 for manager pre-assign; delete only drafts, rejected, or expired-sent (`IsDeletable`); owner sees all nursery quotations; redaction moved into `enrichQuotation`; past `valid_until` → 400, sending an expired draft → 409 |
| Orders: `order_status` on create let an order start COMPLETED/CONFIRMED (skipping the address check); assign-manager accepted any user id; an assigned manager who left kept full access; loaded quantity could exceed the ordered quantity (2026-10-19) | Orders always start PENDING; assignee must be a nursery member and the order not final; the assignee shortcut is gone (membership is checked); loaded > ordered → 400 |
| Dispatches: a buyer could cancel or mark DELIVERED their order's dispatch; a driver could cancel (no reject in V1); `GET /dispatches/code/{code}` showed any trip to any signed-in user (codes are sequential); trip events took empty types and impossible coordinates (2026-10-19) | Status writes need seller staff or the assigned driver, and only staff cancel; code lookup: drivers may preview only unclaimed PENDING trips, everyone else needs dispatch access; event type and coordinates validated |
| Any driver could join any open trip: the join lookup accepted the sequential dispatch code and `POST /dispatches/{id}/accept` took only the id, so trips (and the customer's address) could be guessed (2026-10-26) | Joining needs the trip's QR code, `TRIP-<trip_uuid>`: the lookup matches only `trip_uuid`, and accept requires `trip_code` to match, else 404 |
| Trip invitations as driver invitations: there are no dedicated nursery drivers; any approved GreenRoot driver can accept a trip matching their vehicle type via the trip QR (2026-10-28) | `driver_id` is ignored on create; the trip QR code is the driver invitation with optional `required_vehicle_type` and 72-hour expiry; any approved driver may preview and accept; atomic claim prevents duplicate assignments; staff can renew QR with `POST /dispatches/{id}/new-invite` |
| Trips never recorded when the driver accepted or started: `driver_accepted_at`, `trip_started_at` and `dispatched_at` stayed empty, so every trip timeline had no times (2026-10-28, emulator run) | Accept sets `driver_accepted_at`; IN_TRANSIT sets `trip_started_at`, `trip_started_by_user_id` and `dispatched_at` if unset; `driver_accepted_at` is returned |
| A delivered or cancelled order's delivery address could still be changed, even with `emergency_update` (2026-10-28) | `PUT /orders/:id/delivery` returns 409 for COMPLETED/CANCELLED orders |
| Payments: a buyer could record a SUCCESS payment for their own order or flip their payment to SUCCESS; any user could record a SUCCESS subscription payment; no status machine; payments on cancelled orders (2026-10-19) | Only admin or the selling nursery's owner records/changes order payments; subscription payments admin-only; PENDING → SUCCESS/FAILED/CANCELLED, SUCCESS → REFUNDED (same-status re-save allowed), else 409; cancelled order → 409 |
| GPS points outside IN_TRANSIT returned 400 "invalid input", and the status check ran before the permission check (2026-10-19) | Permission first (403), then 409 `trip_not_in_transit` |
| Redis: `cache:search:gen:*` counters (one per user per module) had no TTL and grew forever; the notifications stream had no cap (2026-10-19) | 24 h TTL refreshed on every bump; `XADD MAXLEN ~100000` |
| Two tables for one idea, one of them pretending to be stock: `nursery_inventory` (quantity, stock status — never reduced by orders) and `nursery_featured_plants` (the real top-20 list). Featured-plant update/delete never checked the item belonged to the URL's nursery; inventory update checked permission against the request's `nursery_id`, so an owner could rewrite another nursery's item; plant-request responses were refused on an unmaintained stock count; nearby sourcing nurseries were sorted by id, not distance, and loaded their plants one query per nursery (2026-10-19) | One table, `nursery_top_items` (plant, size, optional price, display order, notes, ≤5 photos; max 20 per nursery under a row lock; owner + managers edit; permission from the item's own nursery), one module `topitems` (`/top-items`), sourcing reads it in one batched query and sorts by distance; stock check removed; admin page rebuilt for thousands of nurseries |
| `+91 98…` and `98…` created two accounts; `0000000000` was accepted in mock mode; revoked tokens returned 403 instead of 401; suspended users could still refresh tokens (2026-10-19) | One rule, `internal/common/phone.Normalize`, used by auth, SMS, orders, quotations, invites + DB CHECK `ck_users_mobile`; 401 `token_revoked`; refresh re-checks user status |
| Subscription changes (admin pause/expire/cancel, owner renew/pay) reached logged-in owners and managers only at the next token refresh — a paused owner kept working, a paying owner stayed blocked, for up to 15 min (2026-10-21) | Every subscription create/renew/cancel/status change sets `claims:stale:nursery:{id}` in Redis; the auth middleware answers `401 token_stale` to that nursery's tokens issued earlier, and the app's refresh-and-retry picks up the new claims |
| Admin `EXPIRED` left a subscription fully active until its original `end_date`; past the 7-day grace the gate said "subscription required" instead of "expired" (2026-10-21) | Token claims treat an `EXPIRED` row as ended no later than when it was marked; expired rows are no longer dropped from the claims query |
| Owners could renew the free TRIAL (or create TRIAL/FREE) indefinitely; renew could not switch plan, so the app's GROWTH upgrade renewed TRIAL (2026-10-21) | `POST /subscriptions/{id}/renew` takes `plan_id`; non-admins can't create or renew onto `TRIAL`/`FREE` (admins still extend trials) |
| Suspending a user didn't end their sessions: after reactivation, pre-suspension refresh tokens worked again (2026-10-21) | Admin `SUSPENDED`/`DELETED` logs out all of the user's sessions |
| Reactivating a nursery lifted its owner's own (separate) user suspension, and reactivating the owner lifted a still-suspended nursery's owner block (2026-10-21) | Each reactivation clears the owner block only when the other one is not in force |
| Seller capabilities (edit, delete, loading, recall, mark dispatched) were role-based, so an owner/manager buying from another nursery got them on their own purchases (2026-10-21) | Order/quotation/dispatch `BuildCapabilities` drop seller actions when the actor is the record's buyer/customer (quotation recipient also by mobile) |
| Accepting a `DRIVER_INVITE` did nothing (2026-10-21) | Acceptance connects the driver to the nursery as `APPROVED` |
| Quotation → order conversion dropped the buyer's delivery address; orders never returned `customer_name`/`customer_mobile` (2026-10-21) | Conversion copies the buyer's address into `order_delivery_snapshots`; order reads fall back to the buyer's name/mobile |
| A manager who left (or was removed) stayed assigned to quotations/orders: the owner was locked out of editing them and the ex-manager could still open them (2026-10-21) | Leaving/removal unassigns that person's quotations and orders in the nursery |
| A driver could never stop driving (DRIVER blocks every other role); admin deactivation left the role behind; a returning driver skipped review; stopped drivers still got the driver workspace and were blocked from owning (2026-10-21) | `DELETE /drivers/me` (and admin delete) retire the driver: profile INACTIVE, connections DISCONNECTED, DRIVER → BUYER, sessions out; refused on an active trip; re-applying goes back to PENDING; inactive profiles don't count as drivers |
| Closing a nursery left the owner NURSERY_OWNER with an active subscription and its managers MANAGER of a deleted nursery (an ex-owner could then hold MANAGER + NURSERY_OWNER); nothing stopped closing with open work (2026-10-21) | `DELETE /nurseries/{id}` refuses open orders/trips/sent quotations (`nursery_has_active_records`), then releases everyone: owner → BUYER + subscription CANCELLED, managers → BUYER (signed out), drivers disconnected |
| Managers of a suspended nursery silently lost the workspace and saw the buyer app; suspension responses carried no reason (2026-10-21) | Managers get the same block as the owner; the Redis flag stores code + reason + time, returned as `USER_SUSPENDED` / `NURSERY_SUSPENDED` |
| An ad saved before the nursery had coordinates could never publish, though the error says to add the nursery's location (2026-10-21) | Publish falls back to the nursery's coordinates, like create |
| `days_remaining` divided hours by 24 and showed one day less for most of the day (2026-10-21) | Counted in calendar days to the end date |

| A first address was never the default, and deleting the default/primary left none: quotation PDFs lost the customer address and orders lost the nursery pickup address (joins on `is_default` / `is_primary`). The app also sent `is_default` for nursery addresses, so every edit cleared `is_primary` (2026-10-23) | First user/nursery address becomes default/primary; deleting it promotes the newest remaining one; the last nursery address cannot be deleted (409 `last_address`). Journeys pin all three |
| Any signed-in user could list every attachment (licence, delivery and loading photos) and read any by id; any owner/manager/driver could attach a "loading photo" to another nursery's order (2026-10-27) | `GET /attachments` is admin-only; `GET /attachments/:id` admin or the uploader. Loading proof shows only photos uploaded by the selling nursery's owner or active members. Journeys + integration pin it |
| Driver signup had no way to attach the driving licence, though `drivers.licence_photo_url` existed and `apply` accepted any URL (2026-10-23) | Optional licence photo: the app uploads it to the private `attachments` bucket (presign) and sends `licence_photo_url`; apply accepts only a URL in that bucket (400 `invalid_licence_photo`). Admin and the driver get `licence_photo_link` (presigned, 1 h) on driver GET/list/me; the file URL itself is not public. Journeys pin it |
| Any signed-in user could presign an upload into any bucket (public plant/profile images, server PDFs) with any extension and Content-Type; production started without storage TLS or with a database URL that may fall back to plaintext (2026-10-23) | Presign limited to attachments, delivery-photos, market-ads, nursery-logos with per-bucket type lists; extension from the type; Content-Type signed into the PUT URL. `STORAGE_BUCKET_PREFIX` for S3's global bucket names. Production requires `STORAGE_USE_SSL`, an https `STORAGE_PUBLIC_URL` and a TLS-only `DATABASE_URL`. See greenroot-infra/docs/storage-and-data-protection.md |
| Quotation and order notes (documented as internal) and the assigned manager were returned to the buyer; the owner's PDF printed internal notes and was then shared with customers; order PDF rows used ordered quantities while the total used loaded quantities; a nursery owner buying from another nursery got the seller's PDF view; direct uploads had no size limit (2026-10-23) | Notes and assigned manager only on the seller side (`sellerView`); PDFs rebuilt on one engine (`internal/common/pdfdoc`: real Helvetica metrics, wrapping, flowing pages, repeated table header, document info) and identical for every viewer; order rows billed on loaded quantities with Ordered value / Not loaded / Total; presign requires `size` (10 MB images, 20 MB attachments) with `Content-Length` signed |
| The profile photo upload trusted the client's Content-Type and stored anything (e.g. HTML) as an image in a public bucket (2026-10-23) | `POST /users/me/avatar` sniffs the bytes; only JPEG/PNG/WebP/GIF (400 `invalid_image`) |
| Review fixes (2026-10-23): editing an address could clear the default/primary (nothing promoted); a driver could use any private attachment URL as their licence photo; buyers could probe internal order notes via list search | Update only sets the flag (`CASE WHEN $n THEN true ELSE flag END`); attachments keys are `u<uploader>/…` and apply requires the applicant's prefix; `o.notes` is searched only for the seller side and admins (`SearchNotes`) |
