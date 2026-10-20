# Команды разработки. Все Go-команды выполняются в backend/.
.PHONY: run test migrate sqlc db

db:
	docker compose up -d db

run:
	cd backend && go run ./cmd/server

test:
	cd backend && go test ./...

migrate:
	@echo "make migrate появится на шаге 2 (goose)" && exit 1

sqlc:
	@echo "make sqlc появится на шаге 3" && exit 1
