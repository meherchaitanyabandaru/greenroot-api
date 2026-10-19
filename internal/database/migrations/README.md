# Database migrations

**Before launch there are none.** The schema's single source of truth is
[`greenroot-infra/db/postgresql/schema.sql`](../../../../greenroot-infra/db/postgresql/schema.sql).
The 20 migrations written up to 2026-10-19 were folded into it.

## Changing the schema now (pre-launch)

1. Change your local database (psql), or edit `schema.sql` directly.
2. If you changed the database, regenerate the file:
   `greenroot-infra/scripts/dump-schema.sh`
3. Rebuild your database from the file: `greenroot-infra/scripts/reset-dbs.sh`
4. `DATABASE_URL=... go run ./cmd/sqlcheck ./internal/...` and the API tests.

## After launch

Production data can't be rebuilt, so every change becomes a migration here
again, applied by `cmd/migrate` (which already runs in CI and blocks a
production start while one is pending):

- **Name:** `<YYYYMMDD>_<NN>_<description>.up.sql` plus a `.down.sql`
  (manual rollback aid; the runner only runs `up`).
- **Transactions:** each file runs in one transaction with its version record.
  No `BEGIN`/`COMMIT` or psql meta-commands. For `CREATE INDEX CONCURRENTLY`
  put `-- migrate:no-transaction` in the file (plain DDL ending in `;`).
- **Idempotent where practical** (`IF NOT EXISTS`, `ON CONFLICT DO NOTHING`).
- **Never edit an applied migration** — the runner checks checksums.
- **Domain lists:** status columns carry `ck_*` CHECK constraints mirroring the
  Go validators; a new value needs a migration (drop and re-add the
  constraint `NOT VALID`, then `VALIDATE CONSTRAINT`).
- After applying, run `dump-schema.sh` so `schema.sql` stays the full picture.

```bash
go run ./cmd/migrate status   # what is applied / pending
go run ./cmd/migrate up       # apply pending, in order
```

In production `cmd/migrate` uses `MIGRATE_DATABASE_URL` (the
`greenroot_migrator` role from `greenroot-infra/db/postgresql/roles.sql`).
