# GreenRoot API — Architecture

## Design Pattern

```
Request → Handler → Service → Repository → PostgreSQL
```

| Layer | Responsibility |
|---|---|
| **Handler** | Parse request body, path/query params, return standard JSON |
| **Service** | Business rules, role checks, workflow decisions |
| **Repository** | SQL only — no business logic |

**Rules**:
- Modules do not call other modules' repositories directly
- Shared infrastructure belongs in `internal/common`, `internal/database`, `internal/middleware`, or top-level `platform`
- Keep this repo as a **modular monolith** — no microservices, event buses, or Kafka for V1

---

## Authentication

JWT actor extraction is centralized in `internal/common/authctx`.

This helper standardizes:
- Bearer token parsing
- Access-token verification
- User ID parsing
- Role propagation
- IP address capture
- User-agent capture

**Rule**: New protected handlers call `authctx` and pass the actor into the service layer. Never reimplement token parsing in a handler.

---

## Identifier Policy

GreenRoot uses three distinct identifier types:

| Type | Example | Used For |
|---|---|---|
| **Internal ID** | `42` (BIGINT) | DB foreign keys, joins, API routes |
| **Public Code** | `USR-000001`, `PLT-000001`, `ORD-20260622-0001` | Admin UI, support, business ops |
| **External UUID** | (future) | Third-party integration identifiers |

- Public codes are **never** foreign keys
- Public codes **never** replace primary keys
- Code generation: `internal/common/publiccode` backed by `public.public_code_sequences`
- Date-based codes for: requests, orders, dispatches, payments

---

## Module Structure

```text
internal/modules/<name>/
├── handler.go       # HTTP parsing + response
├── routes.go        # Route registration
├── service.go       # Business logic
├── repository.go    # SQL queries only
├── model.go         # Domain types
└── handler_test.go  # Tests
```

Full module reference: [`docs/modules.md`](modules.md)

---

## API Governance

Every production endpoint should define:

| Property | Description |
|---|---|
| Owner module | Which module owns this route |
| Swagger tag | Tag in openapi.yaml |
| Auth requirement | Public / authenticated / admin |
| RBAC policy | Which roles can call it |
| Rate limit | Requests per window |
| Cache policy | Cacheable? TTL? |
| Audit behavior | Does it write to audit log? |
| Request/response examples | In Swagger |

---

## Database Schema

Schema owned by migration files:

```text
internal/database/migrations/
├── 000001_greenroot_baseline.up.sql
├── 000001_greenroot_baseline.down.sql
├── 000002_public_codes.up.sql
└── 000002_public_codes.down.sql
```

Sample/demo data: `../greenroot-infra/db/postgresql/greenroot-seed.sql`

Migration guide: [`docs/migrations.md`](migrations.md)

---

## Logging

Structured JSON logs written to:

```text
logs/
└── YYYY-MM/
    └── YYYY-MM-DD/
        ├── app.log     # Normal application + request logs
        └── errors.log  # Error-level records only
```

Logs older than `LOG_RETENTION_DAYS` (default 90) are removed on API startup.

---

## Public Code Sequences

Table: `public.public_code_sequences`  
Used for concurrent-safe code reservation.

| Entity | Code format |
|---|---|
| Users | `USR-000001` |
| Plants | `PLT-000001` |
| Nurseries | `NUR-000001` |
| Orders | `ORD-20260622-0001` |
| Payments | `PAY-20260622-0001` |
| Dispatches | `DIS-20260622-0001` |
| Plant Requests | `REQ-20260622-0001` |
| Subscriptions | `SUB-000001` |
| Vehicles | `VEH-000001` |
