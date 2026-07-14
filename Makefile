.PHONY: run test tidy fmt vet build smoke integration migrate-up migrate-down migrate-status clean \
        test-unit test-api test-api-orders test-api-dispatches test-api-subscriptions \
        test-all

DB_URL ?= postgres:///greenroot?host=/tmp
DATABASE_URL ?= $(DB_URL)
REDIS_ADDR ?= 127.0.0.1:6379

run:
	DATABASE_URL="$(DATABASE_URL)" REDIS_ADDR="$(REDIS_ADDR)" go run ./cmd/api

test:
	go test ./...

tidy:
	go mod tidy

fmt:
	gofmt -w .

vet:
	go vet ./...

build:
	mkdir -p bin
	go build -o bin/greenroot-api ./cmd/api

smoke:
	./scripts/smoke-test.sh

integration:
	./scripts/integration-test.sh

migrate-up:
	@echo "SQL migrations are squashed for development. Use ../greenroot-infra/scripts/reset-dbs.sh"

migrate-down:
	@echo "SQL migrations are squashed for development. Use ../greenroot-infra/scripts/reset-dbs.sh"

migrate-status:
	@echo "SQL migrations are squashed for development. Canonical schema: ../greenroot-infra/db/postgresql/schema.sql"

clean:
	rm -rf bin coverage.out

# Unit tests (internal/modules service tests)
test-unit:
	go test ./internal/... -v

# API integration tests — requires running server on :8080 with dev seed
test-api:
	go test ./tests/api/... -v -timeout 60s

test-api-orders:
	go test ./tests/api/... -run TestOrder -v -timeout 60s

test-api-dispatches:
	go test ./tests/api/... -run TestDispatch -v -timeout 60s

test-api-subscriptions:
	go test ./tests/api/... -run TestSubscription -v -timeout 60s

# Run everything: unit + API integration
test-all: test-unit test-api
