# GreenRoot — Database Reference

> Last updated: 2026-06-27

---

## Stack

PostgreSQL. The current canonical application schema is `greenroot-api/internal/database/migrations/greenroot_schema.sql`.
Demo/seed data lives in `greenroot-infra/db/postgresql/greenroot-seed.sql`.

Current count from route/schema inspection:

| Item | Count | Source |
|---|---:|---|
| Application tables | 54 | `CREATE TABLE public.*` in `greenroot_schema.sql` |
| Migration bookkeeping table | 1 | `public.schema_migrations`, created by `scripts/migrate.sh` |
| Total DB tables including migration bookkeeping | 55 | Application tables + migration table |

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
└── greenroot_schema.sql    # Full schema: enums, functions, 54 tables, constraints, indexes, reference seeds
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

## SQL Schema Inventory

The schema file is organized into DDL sections, then deferred constraints, indexes, and reference seed data. Application tables are:

| Area | Tables |
|---|---|
| Sequence helper | `public_code_sequences` |
| Reference / lookup | `roles`, `nursery_roles`, `languages`, `plant_sizes`, `plant_categories`, `subscription_plans`, `notification_templates`, `platform_config` |
| Users & identity | `users`, `user_roles`, `user_sessions`, `otp_requests`, `user_activities`, `user_addresses`, `user_subscriptions`, `user_notification_devices` |
| Nursery | `nurseries`, `nursery_applications`, `nursery_addresses`, `nursery_users`, `nursery_drivers`, `nursery_inventory` |
| Plant catalogue | `plants`, `plant_names`, `plant_category_mapping`, `plant_images`, `plant_care_guides`, `plant_requests`, `plant_request_responses` |
| Plant sourcing network | `sourcing_network_members`, `nursery_featured_plants`, `sourcing_posts`, `sourcing_post_responses`, `sourcing_post_photos` |
| Quotations | `quotations`, `quotation_items` |
| Orders | `orders`, `order_items` |
| Dispatch & delivery | `dispatches`, `dispatch_items`, `dispatch_assignments`, `trip_events`, `trip_tracking_links` |
| Vehicles & drivers | `vehicles`, `drivers`, `driver_locations`, `vehicle_locations`, `vehicle_tracking` |
| Invites | `invites` |
| Payments | `payments` |
| Notifications | `notifications` |
| Attachments & audit | `attachments`, `audit_logs` |

Minimal schema application flow:

```sql
-- Applied by the migration script before running schema migrations.
CREATE TABLE IF NOT EXISTS public.schema_migrations (
    version BIGINT PRIMARY KEY,
    dirty BOOLEAN NOT NULL
);
```

```bash
DATABASE_URL='postgres:///greenroot_dev?host=/tmp' make migrate-up
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

There are two seed layers:

| Layer | File | Purpose |
|---|---|---|
| Reference seed | `internal/database/migrations/greenroot_schema.sql` section 18 | Required lookup/config data; safe to re-run with `ON CONFLICT` |
| Demo seed | `../greenroot-infra/db/postgresql/greenroot-seed.sql` | Local/dev sample users, nurseries, plants, quotations, sourcing records, and vehicles |

Reference seed tables in the schema file:
- `roles`
- `nursery_roles`
- `plant_sizes`
- `plant_categories`
- `languages`
- `public_code_sequences`
- `platform_config`
- dev admin user `9000000777`

The demo seed file includes:
- Platform roles and admin user
- Plant sizes, categories, sample plants, and plant-category mappings
- 5 sample nurseries with addresses
- Dev test users for buyer, owner, driver, and manager flows
- 5 sample manager users linked to nurseries
- 5 vehicles
- Sample quotation and quotation items
- Nursery application examples
- Plant sourcing network membership and featured plants
- Public code sequence synchronization

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
