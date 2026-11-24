# Подгружаем переменные User Service (нужен DATABASE_URL для migrate-таргетов).
# `-include` не падает, если файла ещё нет; export пробрасывает их в команды.
-include user/.env
export

.PHONY: proto-gen run-gateway run-user run-url test test-int lint up down migrate-up migrate-down

proto-gen:
	cd shared/proto && buf generate

# Execute
run-gateway:
	go run ./gateway/cmd

run-user:
	go run ./user/cmd

run-url:
	go run ./url/cmd

# Tests
test:
	go test -race -v ./user/...

test-int:
	go test -tags=integration ./integration/...

# Linter
lint:
	golangci-lint run ./user/...

# Docker Compose
up:
	docker compose up -d

down:
	docker compose down

# Migration
migrate-up:
	migrate -path user/migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path user/migrations -database "$(DATABASE_URL)" down 1
