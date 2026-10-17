# Database migrations

The only place schema changes live. Files are embedded in the API binary and
applied by `cmd/migrate`, which records each one in `public.schema_migrations`.

```bash
go run ./cmd/migrate status                 # what is applied / pending
go run ./cmd/migrate up                     # apply pending, in order
go run ./cmd/migrate baseline <version>     # record versions as applied without running them
```

- **Deploy:** run `migrate up` before starting the new API. In production the API **refuses to start** while migrations are pending; elsewhere it logs a warning.
- **Existing databases that predate the runner** (migrated by hand): run `migrate baseline <last version already applied>` once, then `migrate up`. For a database built from the 2026-07-25 `schema.sql`, that is `baseline 20260725_order_documents_verifications`.
- **Fresh local databases** come from `greenroot-infra/db/postgresql/schema.sql`, which includes the `schema_migrations` rows, so nothing is pending.

## Writing a migration

1. **Name:** add `<YYYYMMDD>_<NN>_<description>.up.sql` and a matching `.down.sql` (down files are for manual rollback; the runner only runs `up`).
2. **Transactions:** each file runs in one transaction with its version record. Do not write `BEGIN`/`COMMIT` or psql meta-commands (`\set`).
3. **Concurrent indexes:** for `CREATE INDEX CONCURRENTLY` on live tables, put `-- migrate:no-transaction` in the file. Statements then run one at a time, so keep that file to plain DDL ending in `;`.
4. **Idempotent where practical:** use `IF NOT EXISTS`, and `ON CONFLICT DO NOTHING` / `WHERE NOT EXISTS` for seed rows, so a re-run never overwrites admin edits.
5. **Never edit an applied migration.** The runner checks file checksums and stops on changes; add a new migration instead.
6. **After applying locally:** refresh the fresh-install snapshot with `greenroot-infra/scripts/dump-schema.sh` and check the diff contains only your change.
