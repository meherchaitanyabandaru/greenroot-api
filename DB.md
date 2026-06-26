# GreenRoot — Database Reference

> Last updated: 2026-06-26

---

## Stack

PostgreSQL. Schema owned by ordered migration files in `greenroot-api/internal/database/migrations/`.
Demo/seed data lives in `greenroot-infra/db/postgresql/greenroot-seed.sql`.

---

## Local Setup

```bash
# Create DB and apply migrations
createdb greenroot_dev
cd greenroot-api
DATABASE_URL='postgres:///greenroot_dev?host=/tmp' make migrate-up

# Load demo seed data
psql -v ON_ERROR_STOP=1 -d greenroot_dev \
  -f ../greenroot-infra/db/postgresql/greenroot-seed.sql
```

Migration commands:
```bash
make migrate-up       # Apply pending
make migrate-status   # Check state
make migrate-down     # Roll back (DESTRUCTIVE — drops public schema)
```

---

## Migration Files

```
internal/database/migrations/
├── 000001_greenroot_baseline.up.sql    # Full schema: enums, tables, sequences, indexes
├── 000001_greenroot_baseline.down.sql  # Drops public schema — DESTRUCTIVE
├── 000002_public_codes.up.sql          # Public code sequences
├── 000002_public_codes.down.sql
└── 000005_v1_refactor.up.sql           # owner_user_id on nurseries, invites table, driver columns
```

---

## Migration Rules

| Rule | Detail |
|---|---|
| Never edit applied migrations | Add new changes as the next numbered pair |
| Schema in migrations only | Never put schema in seed files |
| Demo data in infra seed only | Never put demo data in migrations |
| Use `ON_ERROR_STOP=1` | All SQL must be psql-compatible |
| Down migration is destructive | `000001_...down.sql` drops the entire public schema |
| Meaningful names | `000003_<description>.up.sql` / `.down.sql` |

Adding a new migration:
```bash
touch internal/database/migrations/000006_<description>.up.sql
touch internal/database/migrations/000006_<description>.down.sql
```

---

## Identifier Policy

Three distinct ID types used across the platform:

| Type | Example | Used For |
|---|---|---|
| **Internal ID** | `42` (BIGINT) | DB foreign keys, joins, API routes |
| **Public Code** | `USR-000001`, `ORD-20260622-0001` | Admin UI, support, business ops |
| **Trip UUID** | `3f17c2f8-bad3-4e75-...` | Driver/manager/customer onboarding invite codes |

**Rules:**
- Public codes are **never** foreign keys
- Public codes **never** replace primary keys
- Code generation: `internal/common/publiccode` → `public.public_code_sequences`

| Entity | Code Format |
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

---

## Seed Data

The seed file includes:
- Demo data for users, roles, nurseries, inventory, orders, payments, dispatches, requests, notifications, subscriptions, sessions, audit logs
- 5 nurseries, 5 manager users (USR-000002..006), 5 vehicles

**Important prerequisite:** English language record must exist for plant creation:
```sql
INSERT INTO public.languages (language_id, language_code, language_name, is_active, created_at, updated_at)
VALUES (1, 'en', 'English', true, NOW(), NOW())
ON CONFLICT DO NOTHING;
```

---

## Business Rules Enforced in Schema

- `uq_nurseries_owner_user_id` — one user can own at most one nursery (partial, WHERE NOT NULL)
- `uq_drivers_user_id` — proper UNIQUE constraint (not partial index — needed for `ON CONFLICT`)
- Manager membership: `nursery_users.status='ACTIVE'` scopes active memberships; one active per user
- Orders are never hard-deleted — only cancelled (with reason)
- Audit logs are immutable — no UPDATE/DELETE through application

---

## Refresh Baseline (Advanced — Use With Caution)

Only when the local `greenroot` DB intentionally represents the new intended baseline:

```bash
# Dump schema
pg_dump -d greenroot --schema-only --no-owner --no-privileges \
  -f internal/database/migrations/000001_greenroot_baseline.up.sql

# Dump seed data
pg_dump -d greenroot --data-only --column-inserts --no-owner --no-privileges \
  -f ../greenroot-infra/db/postgresql/greenroot-seed.sql
```

---

## Known DB Fixes Applied

| Issue | Fix |
|---|---|
| Migration 000005 was never applied | Applied — adds `owner_user_id` to nurseries, `approval_status`/`profile_status` to drivers, `role`/`status` to `nursery_users`, creates `invites` table |
| `ON CONFLICT (user_id)` on drivers failed | Replaced partial index with proper UNIQUE constraint on `drivers(user_id)` |
| Duplicate driver row for user_id=4 | Removed duplicate |
