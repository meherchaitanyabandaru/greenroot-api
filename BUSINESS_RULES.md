# GreenRoot V1 — Business Rules

> Last updated: 2026-06-28

---

## Platform Purpose

GreenRoot connects nursery businesses with buyers. The mobile app serves all roles (owner, manager, driver, buyer). The admin portal serves platform operations only.

---

## User Roles

| Role | Code | Description |
|---|---|---|
| Super Admin | `SUPER_ADMIN` | Full platform access including admin management |
| Admin | `ADMIN` | Platform ops: catalog, audit, dashboard, user management |
| Nursery Owner | `NURSERY_OWNER` | Owns one nursery; manages inventory, orders, team, customers |
| Manager (Gumastha) | `MANAGER` | Works under one nursery at a time; handles operations |
| Driver | `DRIVER` | Independent; joins trips via UUID/QR |
| Buyer | `BUYER` | Purchases plants; tracks own orders and deliveries |

---

## Account Rules

- One mobile number = one GreenRoot account.
- Every user has exactly one profile.
- OTP is the only login method (mobile + OTP).
- Never physically delete user accounts or business records.

---

## Nursery Rules

- One nursery has exactly one owner (`NURSERY_OWNER`).
- One owner can own at most one nursery.
- Shared ownership is not supported.
- A nursery must be approved by Admin/Super Admin before it can create orders, quotations, or send invites.
- Nursery status: `PENDING_APPROVAL` → `ACTIVE` → `SUSPENDED` / `REJECTED`.
- Any authenticated user (including buyers) may register a nursery application. Drivers cannot.

---

## Manager Rules

- A manager can be active in only one nursery at a time (`uq_manager_one_active_nursery` constraint).
- A manager cannot own a nursery. (`MANAGER_INVITE` rejected if user already owns a nursery.)
- A nursery owner cannot simultaneously be a manager. (`NURSERY_ONBOARDING_INVITE` rejected if user is already a manager.)
- Managers join via UUID/QR invite.
- Manager membership statuses: `ACTIVE`, `SUSPENDED`, `REMOVED`.

---

## Driver Rules

- Drivers are fully independent — they do not belong to any nursery.
- A driver can work trips for multiple nurseries.
- Only one active trip at a time per driver.
- Drivers join individual trips via UUID/QR code.
- Driver profile must be approved before dispatch assignment.

---

## Buyer Rules

- Buyers can view and track their own orders only.
- Buyers can cancel their own PENDING orders (self-cancel, no management permission required).
- Buyers can register a nursery (any authenticated user may apply).
- Buyers cannot create selling orders, edit quotations, or view internal nursery operations.

---

## Order State Machine

```
PENDING → CONFIRMED → LOADING → LOADED → COMPLETED
                                       ↘ PARTIALLY_FULFILLED → COMPLETED
```

All cancel flows go through `POST /orders/:id/cancel` (not a status update).

| Status | Meaning |
|---|---|
| `PENDING` | Created, awaiting nursery confirmation |
| `CONFIRMED` | Nursery confirmed, awaiting loading |
| `LOADING` | Loading in progress |
| `LOADED` | All quantities loaded as ordered |
| `PARTIALLY_FULFILLED` | Loading done, some quantities reduced |
| `COMPLETED` | Delivery confirmed |
| `CANCELLED` | Cancelled before dispatch |

**Invalid direct transitions** (all blocked at API level):
- PENDING → COMPLETED
- CONFIRMED → COMPLETED
- LOADED → PENDING/CONFIRMED/LOADING
- PARTIALLY_FULFILLED → PENDING/CONFIRMED/LOADING

---

## Order Item Editing Rules

Order items (add/edit/remove) are allowed only while the order is in `PENDING`, `CONFIRMED`, or `LOADING`.

Items are locked once the order reaches `LOADED`, `PARTIALLY_FULFILLED`, or `COMPLETED`.

Loading quantities (loaded_quantity per item) can be set only during `LOADING` via `PUT /orders/:id/items/:itemId/loaded-quantity`.

---

## Order Cancel Rules

| From Status | Who Can Cancel |
|---|---|
| `PENDING` | Nursery owner or manager; OR buyer cancelling their own order |
| `CONFIRMED` | Nursery owner or manager only |
| `LOADING` | Nursery owner or manager only |
| `LOADED` | BLOCKED — cannot cancel after dispatch-ready |
| `PARTIALLY_FULFILLED` | BLOCKED — cannot cancel after dispatch-ready |
| `COMPLETED` | BLOCKED |

Cancel reason is optional (empty body accepted).

---

## Order Delete Rules

- Only `PENDING` orders can be hard-deleted.
- All other statuses: cancel first, then optionally archive.
- Orders are never deleted from the database in production.

---

## Partial Fulfillment

When `complete-loading` is called and any item has `loaded_quantity < quantity`, the order transitions to `PARTIALLY_FULFILLED` instead of `LOADED`. Invoice totals are recalculated based on loaded quantities.

---

## Quotation Rules

- Internal quotation: buyer not required.
- Customer quotation: buyer linked.
- Quotations are editable until approved.
- Only the owner can delete a quotation.
- A quotation can be converted to an order.

---

## Dispatch Rules

- Dispatch is created after order reaches `LOADED` or `PARTIALLY_FULFILLED`.
- Driver must accept the trip before it starts.
- Live GPS tracking is enabled once the driver accepts.
- Order becomes read-only once dispatched.

---

## Delivery Rules

- Delivery proof (photos) is required for completion.
- Driver confirms delivery.
- Owner and buyer receive notifications on completion.

---

## Plant Sourcing Network Rules

- Participation is optional (owner opts in).
- Only active, approved nurseries appear in the network.
- Nurseries can publish their top 20 available plants (approximate, not guaranteed stock).
- Managers and owners can post "Need Plant" or "Availability" posts.
- Default search radius: 50 km.
- The sourcing network never creates orders, never exposes customer data, never tracks inventory.

---

## Audit Rules

Every significant business action creates an immutable audit record. Examples:
- Login, logout
- Nursery approval/rejection
- Manager join/leave
- Quotation create/update/approve/convert
- Order create/status-change/cancel/complete
- Loading start/complete
- Dispatch create/complete
- Driver approve
- Payment record

Audit logs are never edited or deleted.

---

## Privacy Rules

| Role | Visible Data |
|---|---|
| Nursery Owner | Everything in their own nursery |
| Manager | Assigned operational data only |
| Driver | Assigned trip data only |
| Buyer | Own orders and deliveries only |

Managers cannot see other managers' data. Managers cannot see customer mobile numbers, addresses, or financial reports.

---

## Inventory (V1 Scope)

Physical inventory management is out of scope for V1. The nursery inventory module stores a loose catalogue of plants a nursery can sell — not a stock ledger. No opening stock, closing stock, or stock transactions.

---

## Core Principles

1. One nursery = one owner.
2. One manager = one active nursery.
3. Drivers are independent.
4. Buyers are independent.
5. No shared nursery ownership.
6. No physical inventory in V1.
7. Order items editable until loading is complete.
8. Completed business records are read-only.
9. Never physically delete business records.
10. Every significant action must be audited.
11. UUID/QR is the standard for invitations and trip joining.
12. DB stores facts, API enforces rules, UI shows only permitted actions.
