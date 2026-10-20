#!/usr/bin/env bash
# Восстановление из бэкапа backup.sh.
#
# Проверка (по умолчанию, рабочую базу не трогает):
#   deploy/restore.sh /var/backups/okuu/okuu-20261020-030000.sql.gz
#   → создаёт временную базу okuu_restore_check, накатывает дамп, печатает число строк
#     в основных таблицах и версию схемы, удаляет временную базу (KEEP=1 — оставить).
#
# Настоящее восстановление в рабочую базу (после аварии):
#   docker compose -f deploy/docker-compose.prod.yml stop app caddy
#   CONFIRM=yes deploy/restore.sh <архив> okuu
#   docker compose -f deploy/docker-compose.prod.yml start app caddy
set -euo pipefail

if [ $# -lt 1 ]; then
	echo "использование: $0 <архив.sql.gz> [база (по умолчанию okuu_restore_check)]" >&2
	exit 2
fi
archive=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
cd "$(dirname "$0")"

env_value() {
	[ -f .env ] || return 0
	grep -E "^$1=" .env | tail -n 1 | cut -d= -f2- | tr -d '\r"'"'"
}

COMPOSE=${COMPOSE:-docker compose -f docker-compose.prod.yml}
PG_SERVICE=${PG_SERVICE:-postgres}
PG_USER=${POSTGRES_USER:-$(env_value POSTGRES_USER)}
PG_USER=${PG_USER:-okuu}
PG_DB=${POSTGRES_DB:-$(env_value POSTGRES_DB)}
PG_DB=${PG_DB:-okuu}
target=${2:-okuu_restore_check}

if ! [[ $target =~ ^[a-z_][a-z0-9_]*$ ]]; then
	echo "имя базы: строчные латинские буквы, цифры и _" >&2
	exit 2
fi
gzip -t "$archive"

psql_admin() { $COMPOSE exec -T "$PG_SERVICE" psql -U "$PG_USER" -d postgres -v ON_ERROR_STOP=1 -q "$@"; }

if [ "$target" = "$PG_DB" ]; then
	if [ "${CONFIRM:-}" != "yes" ]; then
		echo "Это рабочая база «$PG_DB»: её данные будут заменены данными из архива." >&2
		echo "Остановите app и запустите с CONFIRM=yes (см. комментарий в начале скрипта)." >&2
		exit 1
	fi
else
	psql_admin -c "DROP DATABASE IF EXISTS \"$target\"" -c "CREATE DATABASE \"$target\""
fi

echo "восстановление $archive → $target"
gzip -cd "$archive" | $COMPOSE exec -T "$PG_SERVICE" psql -U "$PG_USER" -d "$target" -v ON_ERROR_STOP=1 -q >/dev/null

echo "проверка:"
$COMPOSE exec -T "$PG_SERVICE" psql -U "$PG_USER" -d "$target" -v ON_ERROR_STOP=1 -P pager=off -c "
SELECT (SELECT max(version_id) FROM goose_db_version) AS schema_version,
       (SELECT count(*) FROM centers)       AS centers,
       (SELECT count(*) FROM users)         AS users,
       (SELECT count(*) FROM students)      AS students,
       (SELECT count(*) FROM ledger)        AS ledger,
       (SELECT count(*) FROM notifications) AS notifications,
       (SELECT count(*) FROM audit_log)     AS audit_log;"

if [ "$target" != "$PG_DB" ] && [ "${KEEP:-}" != "1" ]; then
	psql_admin -c "DROP DATABASE \"$target\""
	echo "временная база $target удалена"
fi
echo "готово"
