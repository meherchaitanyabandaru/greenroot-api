# Database migrations

The schema's single source of truth is
[`greenroot-infra/db/postgresql/schema.sql`](../../../../greenroot-infra/db/postgresql/schema.sql),
with reference data in `seed-master.sql`. Every migration written up to
2026-10-31 was folded into those two files, so this folder holds no SQL.

## Changing the schema before launch

1. Change your local database (psql).
2. Regenerate the file: `greenroot-infra/scripts/dump-schema.sh`
3. If the change needs reference rows, add them to `seed-master.sql`.
4. Rebuild your database from the files: `greenroot-infra/scripts/reset-dbs.sh`
5. `DATABASE_URL=... go run ./cmd/sqlcheck ./internal/...` and the API tests.

## After launch: migrations

Live databases cannot be rebuilt, so from then on every change is also a
migration here, applied by `cmd/migrate` (it runs in CI and blocks a
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

Databases built before the fold still list the old versions in
`schema_migrations`; the runner ignores versions it has no file for.
