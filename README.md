# GreenRoot API

Go + PostgreSQL REST API for the GreenRoot farmer/nursery platform.

## Run Locally

```bash
DATABASE_URL='postgres:///greenroot?host=/tmp' \
JWT_SECRET='local-dev-change-me' \
LOG_DIR='/tmp/gr-logs' \
go run ./cmd/api
```

Server: `http://localhost:8080`

Useful endpoints: `/healthz` · `/swagger/` · `/openapi.yaml`

## Configuration

```bash
cp .env.example .env
```

Key variables: `APP_ENV`, `HTTP_PORT`, `DATABASE_URL`, `JWT_SECRET`, `LOG_DIR`, `CORS_ALLOWED_ORIGINS`

For local admin UI: `CORS_ALLOWED_ORIGINS=http://localhost:5173,http://127.0.0.1:5173`

## Commands

```bash
make run          # Start API
make fmt          # Format code
make vet          # Vet code
make test         # Run unit tests
make build        # Build binary
make smoke        # HTTP smoke suite (non-destructive)
make integration  # Full integration suite (disposable DB)
make migrate-up   # Apply migrations
make migrate-status
make migrate-down
```

## Documentation

| Doc | Contents |
|---|---|
| [`docs/architecture.md`](docs/architecture.md) | Design patterns, auth, identifiers, governance |
| [`docs/modules.md`](docs/modules.md) | All 18 modules — routes, responsibilities, hardening |
| [`docs/rbac-matrix.md`](docs/rbac-matrix.md) | RBAC roles and route policies |
| [`docs/testing.md`](docs/testing.md) | Smoke + integration testing guide |
| [`docs/migrations.md`](docs/migrations.md) | DB migration commands and rules |
| [`docs/development-status.md`](docs/development-status.md) | Module status + production hardening backlog |
| [`docs/swagger/`](docs/swagger/) | OpenAPI spec (source of truth for all routes) |

## Full Project Context

See [`../AI_CONTEXT.md`](../AI_CONTEXT.md) for the cross-repo master context.
