DATABASE_URL ?= postgres://devices:devices@localhost:5433/devices?sslmode=disable
MIGRATE := migrate -path migrations -database "$(DATABASE_URL)"

.PHONY: db-up db-down db-reset run migrate-up migrate-down test test-integration lint cover

db-up:
	docker compose up -d --wait db

db-down:
	docker compose down

run: db-up
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/api

migrate-up:
	$(MIGRATE) up

migrate-down:
	$(MIGRATE) down 1

# Local development only: drops everything in the schema, then re-applies all migrations.
db-reset: db-up
	$(MIGRATE) drop -f
	$(MIGRATE) up

test:
	go test ./...

test-integration: db-up
	TEST_DATABASE_URL="$(DATABASE_URL)" go test -count=1 -v ./...

lint:
	golangci-lint run ./...

cover: db-up
	TEST_DATABASE_URL="$(DATABASE_URL)" go test -count=1 -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1
	go tool cover -html=coverage.out -o coverage.html
