# GreenRoot — Database Usage Guide

> **Purpose of this file:** Maps every table to the user types who interact with it,
> what actions they can take, and what API endpoints you need to build.
>
> Use this as the primary reference when designing API routes and RBAC middleware.

---

## User Roles (quick reference)

| Role code | Who they are |
|---|---|
| `SUPER_ADMIN` | Internal GreenRoot team — full platform access |
| `ADMIN` | Operations / support staff — manages platform data |
| `NURSERY_OWNER` | Owns one nursery; sees only their nursery data |
| `MANAGER` | Gumastha — day-to-day nursery ops (assigned by owner) |
| `DRIVER` | Independent delivery driver (freelancer) |
| `BUYER` | Plant buyer placing orders |
| `CUSTOMER` | General user / walk-in (similar to BUYER, no account required for quotes) |
| `SYSTEM` | Background workers, notification service, no HTTP role |

---

## SECTION 1 — Reference / Lookup Tables

These are configuration tables. End users never write to them. Admin manages them; all other roles only read.

---

### `roles`
**Purpose:** Platform-level roles that drive RBAC middleware (which endpoints a user can call).
**Relations:** → `user_roles` (every user gets at least one role from here)

| User Type | Actions |
|---|---|
| SUPER_ADMIN / ADMIN | LIST all roles, toggle `is_active` |
| Everyone else | READ (resolved from JWT — never exposed as a raw list) |

**APIs to build:**
- `GET /admin/roles` — list all (admin portal)
- No public API needed; roles are embedded in auth JWT

---

### `nursery_roles`
**Purpose:** Fine-grained roles scoped to a single nursery (OWNER, MANAGER, OPERATOR, etc.). Stored in `nursery_users.role`.
**Relations:** → `nursery_users` (nursery_role_id FK, nullable in V1)

| User Type | Actions |
|---|---|
| SUPER_ADMIN / ADMIN | LIST all nursery roles |
| NURSERY_OWNER | READ (to display role labels in staff list) |
| Others | No access |

**APIs to build:**
- `GET /admin/nursery-roles` — reference list for admin dropdowns
- `GET /api/v1/nursery-roles` — reference list for owner UI

---

### `languages`
**Purpose:** Supported UI languages (en, hi, te, ta, kn, mr) for multilingual plant names.
**Relations:** → `plant_names` (language_id FK)

| User Type | Actions |
|---|---|
| SUPER_ADMIN / ADMIN | CREATE, UPDATE, toggle active |
| Everyone | READ active languages (for language picker in app) |

**APIs to build:**
- `GET /api/v1/languages` — public list of active languages
- `POST/PATCH /admin/languages` — admin management

---

### `plant_sizes`
**Purpose:** Standard size codes (SEED → EXTRA_LARGE) used on inventory, quotes, and order line items.
**Relations:** → `nursery_inventory`, `quotation_items`, `order_items`, `plant_requests`

| User Type | Actions |
|---|---|
| SUPER_ADMIN / ADMIN | CREATE, UPDATE, toggle active |
| Everyone | READ active sizes (for size picker in app) |

**APIs to build:**
- `GET /api/v1/plant-sizes` — public reference list
- `POST/PATCH /admin/plant-sizes` — admin management

---

### `plant_categories`
**Purpose:** Taxonomy for the plant catalogue (Fruit Trees, Medicinal, Herbs, etc.).
**Relations:** → `plant_category_mapping` (many-to-many with plants)

| User Type | Actions |
|---|---|
| SUPER_ADMIN / ADMIN | CREATE, UPDATE, toggle active |
| BUYER / CUSTOMER | READ (for category filter in mobile app) |
| NURSERY_OWNER / MANAGER | READ (when adding plants to inventory) |

**APIs to build:**
- `GET /api/v1/plant-categories` — public list
- `POST/PATCH /admin/plant-categories` — admin management

---

### `subscription_plans`
**Purpose:** SaaS tiers (FREE, STARTER, PRO) with max_users and max_nurseries limits.
**Relations:** → `user_subscriptions` (plan_id FK)

| User Type | Actions |
|---|---|
| SUPER_ADMIN / ADMIN | CREATE, UPDATE, toggle active, set pricing |
| NURSERY_OWNER | READ available plans (plan selector screen) |
| ADMIN | Assign a plan to an owner via user_subscriptions |

**APIs to build:**
- `GET /api/v1/subscription-plans` — list active plans for owners
- `POST/PATCH /admin/subscription-plans` — admin CRUD

---

### `notification_templates`
**Purpose:** Reusable message templates with `{{variable}}` placeholders for SMS/push/email.
**Relations:** → `notifications` (template_id FK)

| User Type | Actions |
|---|---|
| SUPER_ADMIN / ADMIN | CREATE, UPDATE, toggle active, test send |
| SYSTEM | READ template by code when triggering a notification event |
| Others | No access |

**APIs to build:**
- `GET/POST/PATCH /admin/notification-templates` — admin CRUD

---

### `public_code_sequences`
**Purpose:** Internal atomic counter store for human-readable codes (USR-000001, ORD-20260622-0001). Managed exclusively by the `next_public_code()` DB function.
**Relations:** Used as DEFAULT by 15+ tables

| User Type | Actions |
|---|---|
| SYSTEM (DB function) | READ + INCREMENT on every entity create |
| SUPER_ADMIN | READ for debugging sequence drift (rare) |
| Others | No access — never expose via API |

**APIs to build:** None. This is a DB-internal table.

---

## SECTION 2 — Users & Identity

---

### `users`
**Purpose:** Single identity record for every person on the platform. Login is OTP-on-mobile.
**Relations:** Root table — almost every other table has a FK to `users.user_id`

| User Type | Actions |
|---|---|
| Any (self) | READ own profile, UPDATE name / gender / profile_image_url |
| NURSERY_OWNER | READ profiles of their managers and drivers |
| ADMIN | LIST all users, UPDATE status (ACTIVE / SUSPENDED), SOFT-DELETE (`deleted_at`) |
| SUPER_ADMIN | Same as ADMIN + hard delete |
| Auth module | CREATE on first OTP login, UPDATE `last_login_at` |

**APIs to build:**
- `GET /api/v1/me` — own profile
- `PATCH /api/v1/me` — update own name, gender, avatar
- `GET /admin/users` — paginated list with filters
- `GET /admin/users/:id` — user detail
- `PATCH /admin/users/:id/status` — activate / suspend
- `POST /api/v1/auth/otp/send` + `/verify` — login flow (creates user if new)

---

### `user_roles`
**Purpose:** Many-to-many: a user → platform role(s). Built at registration / invite acceptance.
**Relations:** → `users`, → `roles`

| User Type | Actions |
|---|---|
| Auth module | CREATE on registration or invite accept |
| ADMIN | ASSIGN a role, REMOVE a role from a user |
| All others | READ own roles (embedded in JWT) |

**APIs to build:**
- `POST /admin/users/:id/roles` — assign role
- `DELETE /admin/users/:id/roles/:roleId` — remove role
- Roles resolved automatically in JWT middleware

---

### `user_sessions`
**Purpose:** One row per active login. Tracks device, IP, app version for security audit.
**Relations:** → `users`, ← `user_activities` (session_id)

| User Type | Actions |
|---|---|
| Auth module | CREATE on login, UPDATE `last_activity_at` each request, UPDATE status=EXPIRED on logout |
| Self | LIST own sessions (security settings screen), DELETE (force logout specific device) |
| ADMIN | LIST sessions for a user, force invalidate all |

**APIs to build:**
- `GET /api/v1/me/sessions` — list own active sessions
- `DELETE /api/v1/me/sessions/:id` — logout specific device
- `DELETE /admin/users/:id/sessions` — admin force logout all

---

### `user_activities`
**Purpose:** Append-only event log (what the user did, which entity, when). Never shown to end user.
**Relations:** → `users`, → `user_sessions`

| User Type | Actions |
|---|---|
| SYSTEM | CREATE (logged automatically by app middleware per important action) |
| SUPER_ADMIN / ADMIN | READ for audit / support investigation |
| Others | No access |

**APIs to build:**
- `GET /admin/users/:id/activities` — timeline for support investigation
- No write API; written by app middleware internally

---

### `user_addresses`
**Purpose:** Saved delivery addresses for buyers (home / farm / office). Supports lat/lng pin.
**Relations:** → `users`; read by order creation to pre-fill delivery address

| User Type | Actions |
|---|---|
| BUYER / CUSTOMER (self) | CREATE, LIST own, UPDATE, DELETE, SET default |
| MANAGER | READ buyer's default address when creating an order for them |
| ADMIN | READ all addresses for a user (support) |

**APIs to build:**
- `GET /api/v1/me/addresses` — list own addresses
- `POST /api/v1/me/addresses` — add address
- `PATCH /api/v1/me/addresses/:id` — edit
- `DELETE /api/v1/me/addresses/:id` — remove
- `PATCH /api/v1/me/addresses/:id/default` — set default

---

### `user_subscriptions`
**Purpose:** Links a nursery owner to a subscription plan (start/end date, auto-renew).
**Relations:** → `users`, → `subscription_plans`, ← `payments` (subscription renewal payments)

| User Type | Actions |
|---|---|
| NURSERY_OWNER (self) | READ own subscription status and expiry |
| ADMIN | CREATE (activate plan for owner), UPDATE (change plan, extend, cancel) |
| Billing module | CREATE renewal payment row in `payments` |

**APIs to build:**
- `GET /api/v1/me/subscription` — own active subscription
- `GET /admin/subscriptions` — list all subscriptions
- `POST /admin/users/:id/subscription` — activate plan
- `PATCH /admin/subscriptions/:id` — change plan / status

---

### `user_notification_devices`
**Purpose:** FCM push tokens per device. Enables targeted push notifications.
**Relations:** → `users`; read by notification worker to fan-out push messages

| User Type | Actions |
|---|---|
| Any (self via mobile app) | REGISTER token on app startup (upsert), DEACTIVATE on logout |
| SYSTEM (notification worker) | READ active tokens for a user_id |
| ADMIN | No direct UI; visible in debug tools only |

**APIs to build:**
- `POST /api/v1/me/devices` — register / refresh FCM token
- `DELETE /api/v1/me/devices/:id` — deregister on logout

---

## SECTION 3 — Nursery

---

### `nurseries`
**Purpose:** Core selling entity on the platform. Every order, inventory, and dispatch belongs to a nursery.
**Relations:** ← `nursery_addresses`, ← `nursery_users`, ← `nursery_inventory`, ← `orders`, ← `dispatches`, ← `quotations`

| User Type | Actions |
|---|---|
| NURSERY_OWNER | CREATE own nursery (onboarding), UPDATE name / contact / logo, READ own nursery details |
| MANAGER | READ nursery they are assigned to |
| ADMIN | LIST all nurseries, UPDATE status (ACTIVE / SUSPENDED / PENDING_REVIEW), VIEW details |
| BUYER | READ basic nursery info on order / quotation card |

**APIs to build:**
- `POST /api/v1/nurseries` — owner creates nursery (onboarding)
- `GET /api/v1/me/nursery` — owner reads own nursery
- `PATCH /api/v1/me/nursery` — owner updates nursery details
- `GET /admin/nurseries` — admin list with status filters
- `PATCH /admin/nurseries/:id/status` — admin approve / suspend
- `GET /api/v1/nurseries/:id` — public nursery profile (buyer view)

---

### `nursery_addresses`
**Purpose:** Physical location(s) of a nursery. is_primary marks the main address on maps and quotations.
**Relations:** → `nurseries`

| User Type | Actions |
|---|---|
| NURSERY_OWNER | CREATE, LIST own, UPDATE, DELETE, SET primary |
| MANAGER | READ (for dispatch planning, maps) |
| DRIVER | READ primary address (pickup point for delivery) |
| BUYER | READ primary address on order card |
| ADMIN | READ all addresses for a nursery |

**APIs to build:**
- `GET/POST /api/v1/me/nursery/addresses` — owner manage addresses
- `PATCH /api/v1/me/nursery/addresses/:id` — edit
- `PATCH /api/v1/me/nursery/addresses/:id/primary` — set primary
- `GET /admin/nurseries/:id/addresses` — admin read

---

### `nursery_users`
**Purpose:** Staff roster — managers/gumastha assigned to a nursery. Controls who can operate the nursery on the owner's behalf.
**Relations:** → `nurseries`, → `users`, → `nursery_roles`

| User Type | Actions |
|---|---|
| NURSERY_OWNER | LIST staff, REMOVE staff, VIEW member profile |
| MANAGER (self) | READ own assignment, ACCEPT invite (via invites flow) |
| ADMIN | LIST all staff for any nursery, DEACTIVATE a member |
| Invite module | CREATE row when an invite is accepted |

**APIs to build:**
- `GET /api/v1/me/nursery/members` — owner lists their staff
- `DELETE /api/v1/me/nursery/members/:userId` — owner removes a member
- `GET /admin/nurseries/:id/members` — admin view
- Row created automatically when manager accepts an invite (not a direct write API)

---

### `nursery_drivers`
**Purpose:** Many-to-many bridge between approved drivers and nurseries. A driver requests connection; manager approves.
**Relations:** → `nurseries`, → `users` (driver_user_id, invited_by_user_id, approved_by_user_id)

| User Type | Actions |
|---|---|
| DRIVER | REQUEST connection (scans invite QR), LIST own nursery connections, DISCONNECT |
| NURSERY_OWNER / MANAGER | LIST drivers connected to their nursery, APPROVE / REJECT requests, DISCONNECT driver |
| ADMIN | LIST all connections, force approve / remove |

**APIs to build:**
- `POST /api/v1/nursery-drivers/request` — driver requests connection (via invite UUID)
- `GET /api/v1/me/nursery-connections` — driver lists their nurseries
- `GET /api/v1/me/nursery/drivers` — owner/manager lists their drivers
- `PATCH /api/v1/me/nursery/drivers/:id/approve` — approve connection
- `PATCH /api/v1/me/nursery/drivers/:id/reject` — reject
- `DELETE /api/v1/me/nursery/drivers/:id` — disconnect driver

---

### `nursery_inventory`
**Purpose:** Stock book — available quantity per (nursery × plant × size). The source of truth for what a nursery can sell.
**Relations:** → `nurseries`, → `plants`, → `plant_sizes`; read by quotation and order creation

| User Type | Actions |
|---|---|
| MANAGER | LIST inventory, UPDATE quantity (restock / deduct), ADD new plant-size entry |
| NURSERY_OWNER | READ inventory dashboard, export stock report |
| BUYER | READ availability (is this plant in stock at this nursery?) |
| Order module | READ to validate qty at order creation, DECREMENT on dispatch |
| ADMIN | READ inventory for any nursery, override quantity |

**APIs to build:**
- `GET /api/v1/me/nursery/inventory` — manager/owner lists their inventory
- `POST /api/v1/me/nursery/inventory` — add new item
- `PATCH /api/v1/me/nursery/inventory/:id` — update quantity / status
- `GET /api/v1/nurseries/:id/inventory` — buyer checks availability (active items only)
- `GET /admin/nurseries/:id/inventory` — admin full view

---

## SECTION 4 — Plant Catalogue

---

### `plants`
**Purpose:** Master species catalogue shared across all nurseries. One row per species (by scientific name).
**Relations:** ← `plant_names`, ← `plant_images`, ← `plant_care_guides`, ← `plant_category_mapping`, ← `nursery_inventory`, ← `quotation_items`, ← `order_items`

| User Type | Actions |
|---|---|
| ADMIN | CREATE plant, UPDATE details (common name, type, light/water requirements), toggle active |
| NURSERY_OWNER / MANAGER | READ catalogue to pick plants for inventory |
| BUYER | SEARCH & BROWSE catalogue by name / category / size |
| Everyone | READ individual plant detail page |

**APIs to build:**
- `GET /api/v1/plants` — search / browse (with category, name, size filters)
- `GET /api/v1/plants/:id` — plant detail
- `POST /admin/plants` — admin creates a plant
- `PATCH /admin/plants/:id` — admin updates
- `PATCH /admin/plants/:id/status` — activate / deactivate

---

### `plant_names`
**Purpose:** Plant name in each supported language. Powers multilingual search.
**Relations:** → `plants`, → `languages`

| User Type | Actions |
|---|---|
| ADMIN | CREATE / UPDATE / DELETE translations for a plant |
| Everyone | READ (resolved when displaying a plant in user's preferred language) |

**APIs to build:**
- `GET /api/v1/plants/:id/names` — all language names for a plant
- `POST /admin/plants/:id/names` — add translation
- `PATCH /admin/plants/:id/names/:languageId` — edit translation

---

### `plant_category_mapping`
**Purpose:** Many-to-many: a plant can belong to multiple categories (Neem → Medicinal + Shade Trees).
**Relations:** → `plants`, → `plant_categories`

| User Type | Actions |
|---|---|
| ADMIN | ADD plant to category, REMOVE from category |
| Everyone | READ (resolved when filtering catalogue by category) |

**APIs to build:**
- `POST /admin/plants/:id/categories` — assign category
- `DELETE /admin/plants/:id/categories/:categoryId` — remove
- Category filtering handled in `GET /api/v1/plants` query

---

### `plant_images`
**Purpose:** Photos uploaded to MinIO/S3. is_primary = thumbnail in listings.
**Relations:** → `plants`; image uploaded via presign endpoint first

| User Type | Actions |
|---|---|
| ADMIN | UPLOAD (get presign URL, PUT to S3, then POST record), SET primary, DELETE |
| NURSERY_OWNER | UPLOAD photos for plants they added (if allowed in future) |
| Everyone | READ (image_url returned in plant detail response) |

**APIs to build:**
- `POST /admin/plants/:id/images` — register image after S3 upload
- `PATCH /admin/plants/:id/images/:imageId/primary` — set as primary
- `DELETE /admin/plants/:id/images/:imageId` — remove
- Images returned inline in plant response

---

### `plant_care_guides`
**Purpose:** Structured care guide per plant (sunlight, watering, soil, etc.). One guide per plant.
**Relations:** → `plants`

| User Type | Actions |
|---|---|
| ADMIN | CREATE / UPDATE guide for a plant |
| Everyone | READ on plant detail page (buyer reads before purchase) |

**APIs to build:**
- `GET /api/v1/plants/:id/care-guide` — read guide
- `PUT /admin/plants/:id/care-guide` — upsert guide

---

### `plant_requests`
**Purpose:** Inter-nursery sourcing. A nursery that is short of a plant broadcasts a request to nearby nurseries.
**Relations:** → `nurseries` (requesting), → `users`, → `plants`, → `plant_sizes`; ← `plant_request_responses`

| User Type | Actions |
|---|---|
| MANAGER / NURSERY_OWNER | CREATE request (we need 50 Neem saplings), LIST own nursery's requests, CLOSE / CANCEL |
| Other MANAGERS | READ open requests in their area, RESPOND with availability |
| ADMIN | LIST all requests, monitor fulfilment |

**APIs to build:**
- `POST /api/v1/plant-requests` — create request
- `GET /api/v1/plant-requests/mine` — own nursery's requests
- `GET /api/v1/plant-requests/open` — open requests from other nurseries (to respond to)
- `PATCH /api/v1/plant-requests/:id/close` — mark closed
- `GET /admin/plant-requests` — admin view all

---

### `plant_request_responses`
**Purpose:** Supplier nursery's reply to a plant request — "we have 30 units available".
**Relations:** → `plant_requests`, → `nurseries` (supplier), → `users`

| User Type | Actions |
|---|---|
| MANAGER (supplier nursery) | CREATE response, UPDATE availability |
| MANAGER (requesting nursery) | LIST responses to their request, ACCEPT one response |
| ADMIN | READ all responses |

**APIs to build:**
- `POST /api/v1/plant-requests/:id/responses` — supplier responds
- `GET /api/v1/plant-requests/:id/responses` — requesting nursery views responses
- `PATCH /api/v1/plant-requests/:requestId/responses/:id/accept` — accept a supplier response

---

## SECTION 5 — Quotations

---

### `quotations`
**Purpose:** Price estimate for a customer before it becomes an order. The sales pipeline starts here.
**Status flow:** `DRAFT → SENT → CUSTOMER_ACCEPTED / CUSTOMER_REJECTED → CONVERTED`
**Relations:** → `users` (created_by, assigned_manager, customer), → `nurseries`; ← `quotation_items`; → `orders` (converted_order_id, circular FK)

| User Type | Actions |
|---|---|
| MANAGER | CREATE (DRAFT), ADD items, UPDATE details, SEND to customer, CONVERT to order, CANCEL |
| NURSERY_OWNER | LIST all quotations for their nursery, REVIEW, APPROVE if needed |
| CUSTOMER / BUYER | READ quotation shared with them (PDF link or in-app), ACCEPT or REJECT |
| ADMIN | LIST all quotations, READ detail, force status change (support) |

**APIs to build:**
- `POST /api/v1/quotations` — create (DRAFT)
- `GET /api/v1/quotations` — list (scoped to nursery for manager/owner)
- `GET /api/v1/quotations/:id` — detail
- `PATCH /api/v1/quotations/:id` — update (while DRAFT)
- `POST /api/v1/quotations/:id/send` — mark as SENT (triggers notification to customer)
- `POST /api/v1/quotations/:id/convert` — convert ACCEPTED quote to order
- `POST /api/v1/quotations/:id/cancel` — cancel
- `PATCH /api/v1/quotations/:id/accept` — customer accepts
- `PATCH /api/v1/quotations/:id/reject` — customer rejects
- `GET /admin/quotations` — admin list

---

### `quotation_items`
**Purpose:** Line items on a quotation. Snapshot of plant name and price at quoting time.
**Relations:** → `quotations`, → `plants`

| User Type | Actions |
|---|---|
| MANAGER | ADD item (plant + size + qty + price), UPDATE quantity / price, REMOVE item |
| NURSERY_OWNER | READ items on any of their nursery's quotations |
| CUSTOMER | READ items on quotations shared with them |
| ADMIN | READ |

**APIs to build:**
- `POST /api/v1/quotations/:id/items` — add line item
- `PATCH /api/v1/quotations/:id/items/:itemId` — update qty/price
- `DELETE /api/v1/quotations/:id/items/:itemId` — remove item
- Items returned inline in `GET /api/v1/quotations/:id`

---

## SECTION 6 — Orders

---

### `orders`
**Purpose:** Confirmed sale record. Created from a quotation or directly. Drives inventory reservation, dispatch, and payment.
**Status flow:** `PENDING → CONFIRMED → LOADING → DISPATCHED → DELIVERED` (or `CANCELLED`)
**Relations:** → `nurseries`, → `quotations`, → `users`; ← `order_items`, ← `dispatches`, ← `payments`

| User Type | Actions |
|---|---|
| MANAGER | CREATE order (direct or from quotation), UPDATE customer details, CONFIRM, START loading, COMPLETE loading, CANCEL |
| NURSERY_OWNER | LIST all orders for their nursery, READ detail, CANCEL |
| BUYER / CUSTOMER | LIST own orders (mobile app order history), READ order status and details |
| DRIVER | READ order info (plant list, destination) linked to their dispatch |
| ADMIN | LIST all orders, force status update, VIEW any order |

**APIs to build:**
- `POST /api/v1/orders` — create (direct, no quotation)
- `POST /api/v1/quotations/:id/convert` — creates order from quotation
- `GET /api/v1/orders` — list (scoped: manager/owner sees nursery's orders; buyer sees own)
- `GET /api/v1/orders/:id` — detail
- `PATCH /api/v1/orders/:id/confirm` — confirm order
- `PATCH /api/v1/orders/:id/loading/start` — mark loading started
- `PATCH /api/v1/orders/:id/loading/complete` — mark loading done
- `PATCH /api/v1/orders/:id/cancel` — cancel with reason
- `GET /admin/orders` — admin list with full filters

---

### `order_items`
**Purpose:** Plant line items within an order. The pick list for loading staff.
**Relations:** → `orders`, → `plants`, → `plant_sizes`; ← `dispatch_items`

| User Type | Actions |
|---|---|
| MANAGER | ADD item, UPDATE quantity, REMOVE item (while order is DRAFT/PENDING) |
| Loading staff / MANAGER | READ pick list during loading workflow |
| BUYER / CUSTOMER | READ items in order history |
| ADMIN | READ |

**APIs to build:**
- `POST /api/v1/orders/:id/items` — add item
- `PATCH /api/v1/orders/:id/items/:itemId` — update
- `DELETE /api/v1/orders/:id/items/:itemId` — remove
- Items returned inline in `GET /api/v1/orders/:id`

---

## SECTION 7 — Dispatch & Delivery

---

### `dispatches`
**Purpose:** A single delivery trip for an order. Assigns a vehicle and driver. Carries the customer delivery address.
**Status flow:** `PENDING → CREATED → TRIP_STARTED → DELIVERED` (or `CANCELLED`)
**Relations:** → `orders`, → `nurseries`, → `vehicles`, → `drivers`, → `users`; ← `dispatch_items`, ← `trip_events`, ← `trip_tracking_links`

| User Type | Actions |
|---|---|
| MANAGER | CREATE dispatch (after loading complete), ASSIGN vehicle + driver, RELEASE (change status to CREATED) |
| DRIVER | READ assigned dispatches, START trip, COMPLETE delivery |
| NURSERY_OWNER | READ all dispatches for their nursery |
| CUSTOMER | READ delivery status (via tracking link or order status) |
| ADMIN | LIST all dispatches, reassign driver/vehicle, force status |

**APIs to build:**
- `POST /api/v1/dispatches` — create dispatch for an order
- `PATCH /api/v1/dispatches/:id/assign` — assign vehicle + driver
- `PATCH /api/v1/dispatches/:id/release` — release to driver (status → CREATED)
- `GET /api/v1/dispatches/mine` — driver's assigned dispatches
- `PATCH /api/v1/dispatches/:id/start` — driver starts trip
- `PATCH /api/v1/dispatches/:id/deliver` — driver completes delivery
- `GET /api/v1/orders/:orderId/dispatch` — order's dispatch (for buyer/manager)
- `GET /admin/dispatches` — admin list

---

### `dispatch_items`
**Purpose:** Actual plants loaded onto a specific dispatch. May be partial (some items sent in a later trip).
**Relations:** → `dispatches`, → `order_items`

| User Type | Actions |
|---|---|
| MANAGER | CREATE items (copy from order_items at dispatch creation), UPDATE quantity if partial shipment |
| DRIVER | READ (what's on my truck) |
| ADMIN | READ |

**APIs to build:**
- `POST /api/v1/dispatches/:id/items` — add loaded items
- `PATCH /api/v1/dispatches/:id/items/:itemId` — update quantity
- Items returned inline in `GET /api/v1/dispatches/:id`

---

### `dispatch_assignments`
**Purpose:** Vehicle + driver assignment record for a dispatch. Keeps history if driver is reassigned.
**Relations:** → `dispatches`, → `vehicles`, → `drivers`, → `users` (assigned_by)

| User Type | Actions |
|---|---|
| MANAGER | CREATE assignment (may replace previous if reassigning) |
| ADMIN | READ assignment history |
| Others | Read via dispatch detail |

**APIs to build:**
- Assignment created inline when `PATCH /api/v1/dispatches/:id/assign` is called
- `GET /admin/dispatches/:id/assignments` — assignment history (admin)

---

### `trip_events`
**Purpose:** Chronological log of delivery milestones: TRIP_STARTED, ARRIVED, PHOTO_TAKEN, DELIVERED. Includes GPS + photo for proof.
**Relations:** → `dispatches`, → `users` (created_by_user_id)

| User Type | Actions |
|---|---|
| DRIVER | CREATE events from mobile app (start trip, arrive, take photo, deliver) |
| MANAGER | READ event timeline on dispatch detail |
| CUSTOMER | READ timeline (delivery proof) |
| ADMIN | READ all events for any dispatch |

**APIs to build:**
- `POST /api/v1/dispatches/:id/events` — driver logs an event (with optional GPS + photo_url)
- `GET /api/v1/dispatches/:id/events` — timeline for a dispatch (manager / customer)
- `GET /admin/dispatches/:id/events` — admin full view

---

### `trip_tracking_links`
**Purpose:** UUID-based public tracking link (no login needed). Customer opens in browser to see live driver location.
**Relations:** → `dispatches`, → `users` (customer_user_id)

| User Type | Actions |
|---|---|
| MANAGER | CREATE link when driver departs (auto-created on dispatch start), SHARE via WhatsApp/SMS |
| CUSTOMER (no login) | READ tracking page by UUID (driver position + trip status) |
| ADMIN | LIST active tracking links, expire a link |

**APIs to build:**
- `POST /api/v1/dispatches/:id/tracking-link` — generate / return link
- `GET /track/:uuid` — **public**, no auth — returns driver position + dispatch status
- `GET /admin/tracking-links` — admin view active links

---

## SECTION 8 — Vehicles & Drivers

---

### `vehicles`
**Purpose:** Registry of delivery vehicles. vehicle_number is the licence plate (unique).
**Relations:** ← `dispatches` (vehicle_id), ← `dispatch_assignments`, ← `vehicle_locations`, ← `vehicle_tracking`

| User Type | Actions |
|---|---|
| ADMIN | CREATE, UPDATE (type, capacity, owner), toggle active |
| NURSERY_OWNER | CREATE vehicles owned by their nursery (if future feature), LIST |
| MANAGER | READ available vehicles when creating dispatch |
| ADMIN | LIST all vehicles, VIEW utilisation |

**APIs to build:**
- `GET /api/v1/vehicles` — list available vehicles (for dispatch assignment dropdown)
- `POST /admin/vehicles` — admin registers vehicle
- `PATCH /admin/vehicles/:id` — update details / status
- `GET /admin/vehicles` — admin list with filters

---

### `drivers`
**Purpose:** Driver profile — licence details, approval status. Independent from nurseries.
**Status flow:** `approval_status: PENDING → APPROVED / REJECTED`
**Relations:** → `users` (user_id); ← `nursery_drivers`, ← `dispatches`, ← `driver_locations`

| User Type | Actions |
|---|---|
| DRIVER (self) | CREATE profile (complete licence details, upload photo), READ own profile status |
| ADMIN | LIST all PENDING drivers, APPROVE / REJECT, READ driver detail |
| MANAGER | READ approved drivers connected to their nursery (via nursery_drivers) |
| NURSERY_OWNER | Same as MANAGER |

**APIs to build:**
- `POST /api/v1/me/driver-profile` — driver submits profile
- `PATCH /api/v1/me/driver-profile` — update licence details
- `GET /api/v1/me/driver-profile` — read own profile + approval status
- `GET /admin/drivers` — list with approval_status filter
- `PATCH /admin/drivers/:id/approve` — admin approves
- `PATCH /admin/drivers/:id/reject` — admin rejects
- `GET /admin/drivers/:id` — driver detail

---

### `driver_locations`
**Purpose:** GPS breadcrumb from the driver's phone. Posted every few seconds while on a trip.
**Relations:** → `drivers`

| User Type | Actions |
|---|---|
| DRIVER (mobile app) | CREATE location point (background silent post) |
| ADMIN (live map) | READ latest location per driver |
| Others | Read via vehicle_tracking (dispatch-linked) |

**APIs to build:**
- `POST /api/v1/me/location` — driver posts GPS (called by app in background)
- `GET /admin/drivers/:id/location` — admin live location

---

### `vehicle_locations`
**Purpose:** GPS from vehicle-mounted IoT devices (not driver phone). Includes speed and heading.
**Relations:** → `vehicles`

| User Type | Actions |
|---|---|
| SYSTEM (IoT integration) | CREATE location point |
| ADMIN | READ vehicle locations for fleet map |
| Others | No direct access |

**APIs to build:**
- `POST /api/v1/vehicles/:id/location` — IoT device posts location (API key auth)
- `GET /admin/vehicles/:id/location` — admin fleet tracking

---

### `vehicle_tracking`
**Purpose:** Dispatch-linked GPS — ties a position to a specific active delivery. Powers the customer tracking page.
**Relations:** → `vehicles`, → `drivers`, → `dispatches`

| User Type | Actions |
|---|---|
| DRIVER (mobile app) | CREATE position points while dispatch is TRIP_STARTED |
| CUSTOMER (tracking page) | READ latest position via tracking UUID |
| ADMIN | READ all tracking for a dispatch |

**APIs to build:**
- `POST /api/v1/dispatches/:id/location` — driver posts dispatch-linked GPS point
- `GET /track/:uuid/location` — **public** — latest driver position (for tracking page map)
- `GET /admin/dispatches/:id/tracking` — full path replay

---

## SECTION 9 — Invites

---

### `invites`
**Purpose:** UUID-based invite tokens for onboarding managers, drivers, and buyers without admin intervention.
**invite_type:** `MANAGER` | `DRIVER` | `BUYER`
**Status flow:** `PENDING → ACCEPTED | EXPIRED | REVOKED`
**Relations:** → `users` (invited_by, accepted_by), → `nurseries`

| User Type | Actions |
|---|---|
| NURSERY_OWNER | CREATE manager invite (generates UUID link / QR), LIST pending invites, REVOKE invite |
| MANAGER | CREATE driver invite, LIST pending driver invites, REVOKE |
| DRIVER / new MANAGER | ACCEPT invite (via deep link with UUID) — triggers account creation + nursery_users / nursery_drivers row |
| ADMIN | LIST all invites, force expire |

**APIs to build:**
- `POST /api/v1/invites` — owner/manager creates an invite (returns UUID link)
- `GET /api/v1/invites/mine` — list invites created by me
- `DELETE /api/v1/invites/:id` — revoke
- `GET /api/v1/invites/:uuid` — public — validate and read invite details (no auth; shown on accept screen)
- `POST /api/v1/invites/:uuid/accept` — accept invite (creates user + role + nursery link)
- `GET /admin/invites` — admin list all invites

---

## SECTION 10 — Payments

---

### `payments`
**Purpose:** Every payment transaction — order payment (online or cash) and subscription renewals.
**payment_for:** `ORDER` | `SUBSCRIPTION`
**Status flow:** `PENDING → SUCCESS | FAILED | REFUNDED`
**Relations:** → `orders`, → `user_subscriptions`, → `users` (payer)

| User Type | Actions |
|---|---|
| BUYER / CUSTOMER | INITIATE payment (creates row in PENDING, redirects to Razorpay), VIEW payment history |
| MANAGER | RECORD cash payment manually against an order |
| ADMIN | LIST all payments, RECONCILE, ISSUE refund (update status), VIEW raw Razorpay response |
| Billing module | CREATE subscription renewal payment |

**APIs to build:**
- `POST /api/v1/orders/:id/payment/initiate` — creates PENDING row + Razorpay order
- `POST /api/v1/payments/callback` — Razorpay webhook — verifies signature, updates status to SUCCESS
- `POST /api/v1/orders/:id/payment/cash` — manager records cash payment
- `GET /api/v1/orders/:id/payment` — buyer reads own payment status
- `GET /admin/payments` — admin reconciliation list
- `PATCH /admin/payments/:id/refund` — issue refund

---

## SECTION 11 — Notifications

---

### `notifications`
**Purpose:** One row per message sent. In-app notification history + delivery status tracking.
**notification_type:** `ORDER_UPDATE` | `INVITE` | `DISPATCH_UPDATE` | `SYSTEM` | etc.
**Relations:** → `users` (recipient), → `notification_templates`

| User Type | Actions |
|---|---|
| SYSTEM (notification worker) | CREATE row, UPDATE status (SENT / FAILED) |
| Any user (self) | LIST own notifications (bell badge + notification list), MARK as read, MARK all read |
| ADMIN | LIST notifications for any user, see delivery status, resend failed |

**APIs to build:**
- `GET /api/v1/me/notifications` — list own (with unread count)
- `PATCH /api/v1/me/notifications/:id/read` — mark read
- `PATCH /api/v1/me/notifications/read-all` — mark all read
- `GET /admin/notifications` — admin view all with status filters
- `POST /admin/notifications/:id/resend` — resend failed notification

---

## SECTION 12 — Attachments & Audit

---

### `attachments`
**Purpose:** Polymorphic file store. Any entity (order, nursery, driver) can have multiple files. File goes to MinIO/S3 via presign, record goes here.
**Relations:** → `users` (uploaded_by); entity_type + entity_id form polymorphic key

| User Type | Actions |
|---|---|
| MANAGER | ATTACH purchase order PDF to an order, VIEW attachments on order |
| DRIVER | ATTACH delivery proof photo to a dispatch |
| ADMIN | ATTACH KYC documents to a nursery, VIEW all attachments for an entity |
| NURSERY_OWNER | VIEW attachments for their nursery |

**APIs to build:**
- `POST /api/v1/attachments` — register attachment after S3 upload (body: entity_type, entity_id, file_url)
- `GET /api/v1/attachments?entity_type=order&entity_id=X` — list attachments for an entity
- `DELETE /api/v1/attachments/:id` — remove
- Uses `/api/v1/storage/presign` first to get S3 upload URL

---

### `audit_logs`
**Purpose:** Immutable write-once log of every key mutation. Stores before/after JSON snapshots.
**Relations:** → `users` (changed_by); no FK on table_name/record_id (polymorphic)

| User Type | Actions |
|---|---|
| SYSTEM (app middleware) | CREATE entry on every INSERT/UPDATE/DELETE to key tables |
| SUPER_ADMIN / ADMIN | READ audit trail for a record ("who changed this order price?") |
| Others | No access |

**APIs to build:**
- `GET /admin/audit-logs?table=orders&record_id=X` — admin audit query
- No write API; written by middleware automatically

---

## Entity Relationship Summary

```
users ──────────────────── one-to-many ──► user_roles, user_sessions, user_activities
                                           user_addresses, user_subscriptions
                                           user_notification_devices

users (owner) ──────────── one-to-one ───► nurseries
nurseries ──────────────── one-to-many ──► nursery_addresses, nursery_users
                                           nursery_drivers, nursery_inventory

plants ──────────────────── one-to-many ──► plant_names, plant_images
                                            plant_care_guides
plants ←────────────────── many-to-many ── plant_categories  (via plant_category_mapping)
plants ←────────────────── many-to-many ── nursery_inventory (per nursery × size)

quotations ──────────────── one-to-many ──► quotation_items
quotations ─────────────── one-to-one  ──► orders (converted_order_id)  ← circular FK

orders ──────────────────── one-to-many ──► order_items
orders ──────────────────── one-to-many ──► dispatches
orders ──────────────────── one-to-many ──► payments

dispatches ─────────────── one-to-many ──► dispatch_items, dispatch_assignments
                                           trip_events, trip_tracking_links
                                           vehicle_tracking

drivers ────────────────── one-to-many ──► driver_locations
vehicles ───────────────── one-to-many ──► vehicle_locations

invites ─── accepted by ──► nursery_users OR nursery_drivers  (logic in app layer)
```

---

## User Action Matrix (quick cheatsheet for API design)

| Table | ADMIN | NURSERY_OWNER | MANAGER | DRIVER | BUYER / CUSTOMER |
|---|---|---|---|---|---|
| users | CRUD all | R own | R own | R own | R own, U own profile |
| nurseries | CRUD all, approve | CRUD own | R assigned | — | R basic |
| nursery_inventory | R all | R own | CRUD own | — | R (stock check) |
| plants | CRUD | R | R | — | R, search |
| quotations | R all | R own | CRUD own | — | R own, accept/reject |
| orders | R all, status override | R own | CRUD own, status | R assigned | R own |
| dispatches | R all, reassign | R own | CRUD own | R assigned, start/deliver | R own delivery status |
| drivers | CRUD, approve | R connected | R connected | CRUD own profile | — |
| vehicles | CRUD | R available | R available | R assigned | — |
| payments | R all, reconcile | — | Record cash | — | Initiate, R own |
| invites | R all, expire | C manager invites | C driver invites | Accept | Accept |
| notifications | R all, resend | R own | R own | R own | R own |
| attachments | CRUD any | CRUD own nursery | CRUD own orders | C delivery proof | R own orders |
| audit_logs | R | — | — | — | — |
