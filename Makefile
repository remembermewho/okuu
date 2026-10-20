# Команды разработки. Все Go-команды выполняются в backend/.
SQLC_VERSION := 1.31.1

# Git Bash/MSYS иначе переписывает "/src" в Windows-путь в аргументах docker.
export MSYS_NO_PATHCONV := 1

.PHONY: db run test test-short migrate migrate-status sqlc sqlc-diff image

db:
	docker compose up -d db

run:
	cd backend && go run ./cmd/server

# Интеграционные тесты используют БД из docker compose (make db).
test:
	cd backend && go test ./...

# Только юнит-тесты, без PostgreSQL.
test-short:
	cd backend && go test -short ./...

migrate:
	cd backend && go run ./cmd/server migrate up

migrate-status:
	cd backend && go run ./cmd/server migrate status

# Генерация кода запросов sqlc в Docker (на Windows go install sqlc требует cgo).
sqlc:
	docker run --rm -v "$(CURDIR)/backend:/src" -w /src sqlc/sqlc:$(SQLC_VERSION) generate

# Сгенерированный код совпадает с queries/ (то же проверяет CI).
sqlc-diff:
	docker run --rm -v "$(CURDIR)/backend:/src" -w /src sqlc/sqlc:$(SQLC_VERSION) diff

# Образ API для прода (deploy/docker-compose.prod.yml собирает его сам).
image:
	docker build -t okuu-app:latest backend
