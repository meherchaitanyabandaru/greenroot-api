# GreenRoot API — Database Migrations

## Overview

Schema is owned by ordered migration files. Demo/sample data lives in the infra seed.

```text
internal/database/migrations/
├── 000001_greenroot_baseline.up.sql    # Full schema: enums, tables, sequences, constraints, indexes
├── 000001_greenroot_baseline.down.sql  # Drops public schema — DESTRUCTIVE
├── 000002_public_codes.up.sql          # Public code sequences
└── 000002_public_codes.down.sql
```

Sample/demo data (5 nurseries, 5 manager users, 5 vehicles):
```text
../greenroot-infra/db/postgresql/greenroot-seed.sql
```

---

## Commands

Run from `greenroot-api` root:

```bash
# Apply all pending migrations
DATABASE_URL='postgres:///greenroot_dev?host=/tmp' make migrate-up

# Check migration status
DATABASE_URL='postgres:///greenroot_dev?host=/tmp' make migrate-status

# Roll back (DESTRUCTIVE for baseline — drops public schema)
DATABASE_URL='postgres:///greenroot_dev?host=/tmp' make migrate-down
```

Load demo data after migrating:

```bash
psql -v ON_ERROR_STOP=1 -d greenroot_dev \
  -f ../greenroot-infra/db/postgresql/greenroot-seed.sql
```

---

## Rules

| Rule | Detail |
|---|---|
| Never edit applied migrations | Add new changes as the next numbered pair |
| Schema stays in migrations | Never put schema in seed files |
| Demo data stays in infra seed | Never put demo data in migrations |
| Use `ON_ERROR_STOP=1` | All SQL must be psql-compatible |
| Down migration is destructive | `000001_...down.sql` drops the entire public schema |

---

## Adding a New Migration

```bash
# Name format: 000003_<description>.up.sql / .down.sql
touch internal/database/migrations/000003_add_feature.up.sql
touch internal/database/migrations/000003_add_feature.down.sql
```

Write forward SQL in `.up.sql`, rollback SQL in `.down.sql`.

---

## Migration Tracker

```text
public.schema_migrations   # Maintained by the migration runner
```

---

## Refresh Baseline (Advanced — Use With Caution)

Only when the local `greenroot` database intentionally represents the new baseline:

```bash
pg_dump -d greenroot --schema-only --no-owner --no-privileges \
  -f internal/database/migrations/000001_greenroot_baseline.up.sql

pg_dump -d greenroot --data-only --column-inserts --no-owner --no-privileges \
  -f ../greenroot-infra/db/postgresql/greenroot-seed.sql
```
