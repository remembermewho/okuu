# Образ API Окуу: только backend (HTTP API, вебхуки, фоновые задачи). Фронтенд отдаёт Caddy.
# Сборка из корня репозитория: docker build -t okuu-app backend
# Миграции и часовые пояса встроены в бинарник; при старте миграции применяются сами.

FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# distroless/static: без shell и пакетного менеджера, пользователь nonroot (uid 65532).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
USER nonroot:nonroot
ENV APP_ENV=prod HTTP_ADDR=:8080
EXPOSE 8080
# В образе нет curl — проверку делает сам сервер: GET /readyz (БД, миграции, планировщик).
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["/server", "healthcheck"]
ENTRYPOINT ["/server"]
