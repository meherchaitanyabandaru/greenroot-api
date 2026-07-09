.PHONY: run test tidy fmt vet build smoke integration migrate-up migrate-down migrate-status clean \
        test-unit test-api test-api-orders test-api-dispatches test-api-subscriptions \
        test-db test-db-orders test-db-dispatches test-db-subscriptions test-db-quotations test-db-plant-requests \
        test-all

DB_URL ?= postgres:///greenroot?host=/tmp

run:
	go run ./cmd/api

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
	./scripts/migrate.sh up

migrate-down:
	./scripts/migrate.sh down

migrate-status:
	./scripts/migrate.sh status

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

# DB integrity checks — requires local greenroot DB
test-db:
	@for f in tests/db/*.sql; do \
		echo ""; \
		echo ">>> $$f"; \
		psql '$(DB_URL)' -f "$$f"; \
	done

test-db-orders:
	psql '$(DB_URL)' -f tests/db/orders.sql

test-db-dispatches:
	psql '$(DB_URL)' -f tests/db/dispatches.sql

test-db-subscriptions:
	psql '$(DB_URL)' -f tests/db/subscriptions.sql

test-db-quotations:
	psql '$(DB_URL)' -f tests/db/quotations.sql

test-db-plant-requests:
	psql '$(DB_URL)' -f tests/db/plant_requests.sql

# Run everything: unit + DB checks + API integration
test-all: test-unit test-db test-api
