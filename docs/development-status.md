# GreenRoot API — Development Status

## Module Status

| Module | Status | Notes |
|---|---|---|
| Health | ✅ Complete | Liveness/readiness endpoints |
| Auth | ✅ Complete | OTP mock, JWT access/refresh, sessions |
| Users | ✅ Complete | Profile, roles, addresses, sessions |
| Plants | ✅ Complete | CRUD, categories, images, care guides |
| Nurseries | ✅ Complete | CRUD, addresses, nursery users |
| Inventory | ✅ Complete | Nursery and plant inventory |
| Plant Requests | ✅ Complete | Request/response flow and matching foundation |
| Orders | ✅ Complete | Orders and order items |
| Payments | ✅ Complete | Manual order payments and subscription payment records |
| Subscriptions | ✅ Complete | Plans, create, renew, cancel, status |
| Vehicles | ✅ Complete | Vehicle registry and status |
| Drivers | ✅ Complete | Driver registry and location updates |
| Dispatches | ✅ Complete | Dispatch creation, items, status |
| Tracking | ✅ Complete | Tracking point creation, latest/history reads |
| Notifications | ✅ Complete | Notifications, devices, templates, mock sender |
| Attachments | ✅ Complete | S3-ready attachment metadata |
| Audit | ✅ Complete | Admin audit log search |
| Admin | ✅ Complete | Dashboard summary |

---

## Provider Status (Intentionally Deferred)

| Provider | Status | Notes |
|---|---|---|
| FCM (Firebase Cloud Messaging) | 🔶 Mock | Real credentials needed |
| S3 File Storage | 🔶 Mock | Pre-signed URL logic pending |
| Razorpay/PayU Payments | 🔶 Mock | Gateway metadata stored, capture pending |
| OTP Provider | 🔶 Mock | `123456` hardcoded for dev |

---

## Admin UI Integration Status

See [`../greenroot-admin/docs/api-integration-matrix.md`](../../greenroot-admin/docs/api-integration-matrix.md) for UI coverage per module.

---

## Next Hardening Priorities (Before Production)

### Must Do Before Launch

1. **Security**
   - [ ] Rate limiting on all routes
   - [ ] Request size limits
   - [ ] Security headers (HSTS, X-Frame-Options, etc.)
   - [ ] CORS policy review per environment
   - [ ] Consistent input validation across all handlers
   - [ ] RBAC enforcement audit — all routes
   - [ ] Login brute-force protection

2. **Providers**
   - [ ] Real FCM sender
   - [ ] S3 pre-signed upload/download
   - [ ] Razorpay/PayU subscription payment capture + webhooks

3. **Testing**
   - [ ] Expand integration tests — edge cases and negative paths
   - [ ] Unauthorized access tests for every protected route
   - [ ] Failure-path tests

4. **CI/CD**
   - [ ] `gofmt` check
   - [ ] `go vet ./...`
   - [ ] `go test ./...`
   - [ ] `go build ./cmd/api`
   - [ ] OpenAPI validation

5. **Database**
   - [ ] Review and add missing indexes
   - [ ] Verify all foreign keys and cascade rules
   - [ ] Slow-query logging guidance

6. **Observability**
   - [ ] Structured logging (already in place — verify completeness)
   - [ ] Request IDs on all responses
   - [ ] Metrics endpoint
   - [ ] Error tracking hooks (Sentry or equivalent)

7. **Deployment**
   - [ ] Production Docker configuration
   - [ ] Docker Compose production stack
   - [ ] Environment variable reference doc
   - [ ] HTTPS reverse proxy guidance
   - [ ] Health checks in deployment config

8. **Backups**
   - [ ] PostgreSQL backup scripts
   - [ ] Restore scripts and documentation

9. **Documentation**
   - [ ] Response schemas for every Swagger endpoint
   - [ ] Request examples in Swagger
   - [ ] Auth/role notes per Swagger tag
   - [ ] Production deployment guide
   - [ ] Operations runbook

10. **Background Jobs**
    - [ ] Job queue abstraction
    - [ ] Async notifications
    - [ ] Async cleanup jobs
    - [ ] Retry support

---

## Seed & Schema Baseline

Refresh commands (only when local `greenroot` is the new intended baseline):

```bash
# Dump schema
pg_dump -d greenroot --schema-only --no-owner --no-privileges \
  -f internal/database/migrations/000001_greenroot_baseline.up.sql

# Dump seed data
pg_dump -d greenroot --data-only --column-inserts --no-owner --no-privileges \
  -f ../greenroot-infra/db/postgresql/greenroot-seed.sql
```
