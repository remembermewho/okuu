# Образ caddy для прода: HTTPS (Let's Encrypt) + статика приложения и лендинга.
# Фронтенд и лендинг собираются здесь же: на сервере не нужен Node, а образ — это одна версия сайта целиком
# (обновление и откат — сменой образа, без ручной пересборки frontend/dist и landing/dist).
#
# Собирает docker-compose.prod.yml (контекст — корень репозитория). Вручную:
#   docker build -f deploy/caddy.Dockerfile --build-arg SITE_URL=https://okuu.kg --build-arg APP_URL=https://app.okuu.kg .
# Что попадает в контекст — deploy/caddy.Dockerfile.dockerignore.

FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
# API — на том же домене (/api/...), переменных окружения у сборки нет.
RUN npm run build

FROM node:22-alpine AS landing
# Адреса лендинга и приложения для ссылок, canonical и sitemap (https://LANDING_DOMAIN, https://APP_DOMAIN).
# --prod не соберёт с пустыми, http:// или localhost.
ARG SITE_URL
ARG APP_URL
WORKDIR /src/landing
COPY landing/ ./
RUN node build.mjs --prod

FROM caddy:2.8-alpine
COPY deploy/Caddyfile /etc/caddy/Caddyfile
COPY --from=frontend /src/frontend/dist /srv/frontend
COPY --from=landing /src/landing/dist /srv/landing
