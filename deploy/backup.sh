#!/usr/bin/env bash
# Бэкап базы: pg_dump | gzip в BACKUP_DIR, архивы старше KEEP_DAYS дней удаляются.
# Запуск (cron, раз в сутки в 03:00 — см. README «Деплой на VPS»):
#   /opt/okuu/deploy/backup.sh >> /var/log/okuu-backup.log 2>&1
# Печатает путь к новому архиву. Ненулевой код — бэкап не сделан (архив-обрывок удаляется).
#
# Переменные (по умолчанию — из deploy/.env и для docker-compose.prod.yml):
#   BACKUP_DIR (/var/backups/okuu), KEEP_DAYS (30), COMPOSE, PG_SERVICE (postgres),
#   POSTGRES_USER, POSTGRES_DB.
set -euo pipefail

cd "$(dirname "$0")"

# Из .env берём только нужные переменные, не исполняя файл.
env_value() {
	[ -f .env ] || return 0
	grep -E "^$1=" .env | tail -n 1 | cut -d= -f2- | tr -d '\r"'"'"
}

BACKUP_DIR=${BACKUP_DIR:-$(env_value BACKUP_DIR)}
BACKUP_DIR=${BACKUP_DIR:-/var/backups/okuu}
KEEP_DAYS=${KEEP_DAYS:-30}
COMPOSE=${COMPOSE:-docker compose -f docker-compose.prod.yml}
PG_SERVICE=${PG_SERVICE:-postgres}
PG_USER=${POSTGRES_USER:-$(env_value POSTGRES_USER)}
PG_USER=${PG_USER:-okuu}
PG_DB=${POSTGRES_DB:-$(env_value POSTGRES_DB)}
PG_DB=${PG_DB:-okuu}

mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"
umask 077

file="$BACKUP_DIR/okuu-$(date -u +%Y%m%d-%H%M%S).sql.gz"
tmp="$file.part"
trap 'rm -f "$tmp"' EXIT

# --clean --if-exists: дамп можно накатить и на существующую базу (restore.sh).
$COMPOSE exec -T "$PG_SERVICE" pg_dump -U "$PG_USER" -d "$PG_DB" --no-owner --no-privileges --clean --if-exists |
	gzip -9 >"$tmp"
gzip -t "$tmp" # архив читается целиком
if [ "$(gzip -cd "$tmp" | head -c 1024 | wc -c)" -eq 0 ]; then
	echo "бэкап пустой — pg_dump ничего не вернул" >&2
	exit 1
fi
mv "$tmp" "$file"
trap - EXIT

find "$BACKUP_DIR" -maxdepth 1 -name 'okuu-*.sql.gz' -mtime +"$KEEP_DAYS" -print -delete | sed 's/^/удалён старый: /' >&2
echo "$file"
