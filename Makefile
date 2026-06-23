.PHONY: run test tidy fmt vet build smoke integration migrate-up migrate-down migrate-status clean

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
