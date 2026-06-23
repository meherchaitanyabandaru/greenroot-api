# GreenRoot API — Modules Reference

Each module owns its HTTP handlers, request/response DTOs, service logic, repository interface, and tests.

**Pattern**: `Handler → Service → Repository → PostgreSQL`  
**Auth**: All protected handlers use `internal/common/authctx` for JWT actor extraction — never reimplement.  
**Cross-cutting infra**: Keep in `internal/common`, `internal/database`, `internal/middleware`, or top-level `platform`.

---

## Module Layout (per module)

```text
internal/modules/<name>/
├── handler.go
├── routes.go
├── service.go
├── repository.go
├── model.go
└── handler_test.go
```

---

## Modules

### auth
**Status**: Complete  
**Responsibilities**: OTP login, JWT access/refresh tokens, sessions, activities, current-user lookup.  
**Main Routes**:
```
POST /api/v1/auth/send-otp
POST /api/v1/auth/verify-otp
POST /api/v1/auth/refresh-token
POST /api/v1/auth/logout
GET  /api/v1/auth/me
```
**Hardening**: Real OTP provider, account recovery.

---

### users
**Status**: Complete  
**Responsibilities**: User profile, addresses, roles, sessions, gender support.  
**Main Routes**:
```
GET /api/v1/users/me
GET/PUT /api/v1/users/:id
GET /api/v1/users/:id/addresses
GET /api/v1/users/:id/roles
GET /api/v1/users/:id/sessions
```
**Hardening**: Admin user moderation, preferences.

---

### plants
**Status**: Complete  
**Responsibilities**: Plant catalog CRUD, categories, images, care guides, search, filters, pagination.  
**Main Routes**:
```
GET/POST /api/v1/plants
GET/PUT/DELETE /api/v1/plants/:id
GET/POST /api/v1/plants/:id/images
GET/PUT /api/v1/plants/:id/care-guide
GET /api/v1/plant-categories
```
**Hardening**: Multilingual plant names, popularity metrics.

---

### nurseries
**Status**: Complete  
**Responsibilities**: Nursery CRUD, addresses, nursery users.  
**Main Routes**:
```
GET/POST /api/v1/nurseries
GET/PUT/DELETE /api/v1/nurseries/:id
GET/POST /api/v1/nurseries/:id/addresses
GET/POST /api/v1/nurseries/:id/users
```
**Hardening**: Approval workflow, analytics.

---

### inventory
**Status**: Complete  
**Responsibilities**: Nursery and plant inventory management.  
**Main Routes**:
```
GET/POST /api/v1/inventory
GET/PUT/DELETE /api/v1/inventory/:id
GET /api/v1/nurseries/:id/inventory
GET /api/v1/plants/:id/inventory
```
**Hardening**: Out-of-stock transitions, bulk import.

---

### requests (Plant Requests)
**Status**: Complete  
**Responsibilities**: Request/response flow and matching foundation.  
**Main Routes**:
```
GET/POST /api/v1/plant-requests
GET/PUT/DELETE /api/v1/plant-requests/:id
GET /api/v1/plant-requests/:id/responses
POST /api/v1/plant-requests/:id/cancel
```
**Hardening**: Response matching workflow, insufficient inventory negative cases.

---

### orders
**Status**: Complete  
**Responsibilities**: Orders, order items, status updates, role-scoped access.  
**Main Routes**:
```
GET/POST /api/v1/orders
GET/DELETE /api/v1/orders/:id
PUT /api/v1/orders/:id/status
GET/POST /api/v1/orders/:id/items
GET /api/v1/orders/:id/dispatches
```
**Hardening**: Stronger order lifecycle transitions.

---

### payments
**Status**: Complete  
**Responsibilities**: Manual order payment ledger and subscription payment records with gateway-ready metadata.  
**Main Routes**:
```
GET/POST /api/v1/payments
POST /api/v1/payments/manual
PUT /api/v1/payments/:id/status
GET /api/v1/orders/:id/payments
GET /api/v1/subscriptions/:id/payments
```
**Hardening**: Razorpay/PayU capture, refunds, webhooks.

---

### subscriptions
**Status**: Complete  
**Responsibilities**: Subscription plans and user subscription lifecycle.  
**Main Routes**:
```
GET /api/v1/subscription-plans
GET/POST /api/v1/subscriptions
GET /api/v1/subscriptions/me
GET /api/v1/subscriptions/:id
PUT /api/v1/subscriptions/:id/status
POST /api/v1/subscriptions/:id/renew
POST /api/v1/subscriptions/:id/cancel
```
**Hardening**: Real payment capture, plan administration.

---

### vehicles
**Status**: Complete  
**Responsibilities**: Vehicle registry and lifecycle status.  
**Main Routes**:
```
GET/POST /api/v1/vehicles
GET/PUT/DELETE /api/v1/vehicles/:id
```
**Hardening**: Availability, maintenance history, utilization reports.

---

### drivers
**Status**: Complete  
**Responsibilities**: Driver registry and driver location updates.  
**Main Routes**:
```
GET/POST /api/v1/drivers
GET/PUT/DELETE /api/v1/drivers/:id
POST /api/v1/drivers/:id/location
```
**Hardening**: Driver app permissions, availability schedules.

---

### dispatches
**Status**: Complete  
**Responsibilities**: Order fulfillment dispatches, dispatch items, and status updates.  
**Main Routes**:
```
GET/POST /api/v1/dispatches
GET /api/v1/dispatches/:id
PUT /api/v1/dispatches/:id/status
POST /api/v1/dispatches/:id/items
GET /api/v1/orders/:id/dispatches
```
**Hardening**: Notification hooks, assignment workflows.

---

### tracking
**Status**: Complete  
**Responsibilities**: Vehicle, driver, and dispatch tracking history/latest location.  
**Main Routes**:
```
POST /api/v1/tracking
GET /api/v1/tracking/vehicle/:id/latest
GET /api/v1/tracking/vehicle/:id/history
GET /api/v1/tracking/driver/:id/latest
GET /api/v1/tracking/dispatch/:id/latest
```
**Hardening**: Live map feeds, driver-side authorization tightening.

---

### notifications
**Status**: Complete  
**Responsibilities**: User notifications, FCM device tokens, notification templates with mock sender.  
**Main Routes**:
```
GET/POST /api/v1/notifications
PUT /api/v1/notifications/:id/read
DELETE /api/v1/notifications/:id
GET/POST /api/v1/notification-devices
GET/POST /api/v1/notification-templates
```
**Hardening**: Real Firebase Cloud Messaging provider, event-triggered sends.

---

### attachments
**Status**: Complete  
**Responsibilities**: S3-ready file metadata linked to domain entities.  
**Main Routes**:
```
GET/POST /api/v1/attachments
GET/DELETE /api/v1/attachments/:id
```
**Hardening**: Pre-signed S3 upload/download, malware/content checks.

---

### audit
**Status**: Complete  
**Responsibilities**: Admin audit-log search over records written by domain modules.  
**Main Routes**:
```
GET /api/v1/audit-logs
```
**Hardening**: Export support, richer date/entity filters.

---

### admin
**Status**: Complete  
**Responsibilities**: Admin-only platform overview APIs.  
**Main Routes**:
```
GET /api/v1/admin/dashboard
GET /api/v1/admin/users
```
**Hardening**: Dashboard queries read-only, reuse domain services for future screens.

---

### health
**Status**: Complete  
**Main Routes**:
```
GET /health
GET /healthz
GET /readyz
```
