#!/usr/bin/env bash
# Обновление Окуу на сервере до последней версии из GitHub (docs/DEPLOY.md, «Обновление версии»).
# Под пользователем okuu:
#   cd /opt/okuu && bash deploy/update.sh
#
# Порядок: входящие коммиты (и вопрос «обновить?») → бэкап базы (deploy/backup.sh) → текущие образы
# помечаются :prev → git pull --ff-only → docker compose up -d --build → ждём healthy у app → состояние и
# хвост лога. Если что-то пошло не так — печатает команды отката на прежнюю версию.
#
# Переменные: YES=1 — не спрашивать подтверждение; HEALTH_TIMEOUT — сколько секунд ждать healthy (120);
# COMPOSE — команда compose; APP_IMAGE, WEB_IMAGE — имена образов (как у compose: из deploy/.env,
# по умолчанию okuu-app:latest и okuu-web:latest).
set -euo pipefail

cd "$(dirname "$0")/.."
root=$(pwd)

# Из deploy/.env берём только нужные переменные, не исполняя файл (как в backup.sh).
env_value() {
	[ -f deploy/.env ] || return 0
	grep -E "^$1=" deploy/.env | tail -n 1 | cut -d= -f2- | tr -d '\r"'"'"
}

COMPOSE=${COMPOSE:-docker compose -f $root/deploy/docker-compose.prod.yml}
HEALTH_TIMEOUT=${HEALTH_TIMEOUT:-120}
APP_IMAGE=${APP_IMAGE:-$(env_value APP_IMAGE)}
APP_IMAGE=${APP_IMAGE:-okuu-app:latest}
WEB_IMAGE=${WEB_IMAGE:-$(env_value WEB_IMAGE)}
WEB_IMAGE=${WEB_IMAGE:-okuu-web:latest}
if ! [[ $HEALTH_TIMEOUT =~ ^[1-9][0-9]*$ ]]; then
	echo "HEALTH_TIMEOUT: целое число секунд, получено «$HEALTH_TIMEOUT»" >&2
	exit 2
fi

prev_of() { echo "${1%:*}:prev"; }
step() { printf '\n== %s\n' "$*"; }

# --- Git: на ветке, без локальных правок, обновление — перемотка вперёд.
if ! branch=$(git symbolic-ref --quiet --short HEAD); then
	echo "Код не на ветке (git checkout <коммит> после отката?). Вернитесь на ветку: git checkout main" >&2
	exit 1
fi
if ! upstream=$(git rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null); then
	echo "У ветки $branch нет удалённой ветки для обновления (git branch -u origin/$branch)" >&2
	exit 1
fi
if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
	echo "В $root изменены файлы из репозитория — git pull может не пройти. Посмотрите: git status, git diff" >&2
	exit 1
fi

step "Новые коммиты ($upstream)"
git fetch --quiet
old=$(git rev-parse --short HEAD)
if [ "$(git rev-list --count "HEAD..$upstream")" -eq 0 ]; then
	echo "Новых коммитов нет, работает $(git log -1 --format='%h %s')."
	echo "Пересобрать без изменений кода: $COMPOSE up -d --build"
	exit 0
fi
if ! git merge-base --is-ancestor HEAD "$upstream"; then
	echo "На сервере есть коммиты, которых нет в $upstream, — git pull --ff-only не пройдёт. Посмотрите: git log $upstream..HEAD" >&2
	exit 1
fi
new=$(git rev-parse --short "$upstream")
git log --oneline --no-decorate "HEAD..$upstream"
migrations=$(git diff --name-only --diff-filter=A "HEAD..$upstream" -- backend/migrations)
if [ -n "$migrations" ]; then
	echo
	echo "Новые миграции — применятся при старте, назад не откатываются:"
	echo "$migrations" | sed 's/^/  /'
fi
if [ -t 0 ] && [ "${YES:-}" != 1 ]; then
	echo
	read -r -p "Обновить $old → $new? [y/N] " answer
	if ! [[ $answer =~ ^[yYдД] ]]; then
		echo "Отменено, ничего не изменено."
		exit 0
	fi
fi

# С этого места при ошибке печатаем, что с версией и как вернуть прежнюю.
pulled=
backup=
declare -a prev_images=()
rollback_help() {
	local status=$?
	[ "$status" -eq 0 ] && return
	echo >&2
	if [ -z "$pulled" ]; then
		echo "Обновление остановлено до git pull: код и контейнеры не менялись, работает $old." >&2
		return
	fi
	{
		echo "Обновление $old → $new не удалось (код $status). Вернуть прежнюю версию:"
		echo
		echo "  cd $root"
		if [ "${#prev_images[@]}" -eq 2 ]; then
			echo "  docker tag $(prev_of "$APP_IMAGE") $APP_IMAGE"
			echo "  docker tag $(prev_of "$WEB_IMAGE") $WEB_IMAGE"
			echo "  $COMPOSE up -d --no-build"
			echo "  git checkout $old"
		else
			echo "  git checkout $old"
			echo "  $COMPOSE up -d --build      # образов :prev нет — пересборка прежнего коммита"
		fi
		echo "  $COMPOSE ps"
		echo
		echo "Если новая версия успела испортить данные — восстановить бэкап перед обновлением:"
		echo "  $COMPOSE stop app caddy"
		echo "  CONFIRM=yes bash deploy/restore.sh $backup okuu"
		echo "  $COMPOSE start app caddy"
		echo
		echo "Подробно — docs/DEPLOY.md, «Откат». Потом, когда исправление будет в GitHub: git checkout $branch"
	} >&2
}
trap rollback_help EXIT

step "Бэкап базы"
backup=$(bash deploy/backup.sh)
echo "$backup"

step "Текущие образы → :prev (для отката)"
for img in "$APP_IMAGE" "$WEB_IMAGE"; do
	if docker image inspect "$img" >/dev/null 2>&1; then
		docker tag "$img" "$(prev_of "$img")"
		prev_images+=("$img")
		echo "$img → $(prev_of "$img")"
	else
		echo "образа $img нет (первый запуск?) — откат будет только пересборкой прежнего коммита"
	fi
done

step "git pull"
git pull --ff-only --quiet
pulled=1
git log -1 --oneline --no-decorate

step "Сборка и перезапуск"
# caddy ждёт healthy у app (depends_on), так что up обычно возвращается, когда app уже здоров;
# ошибка сборки или нездоровый app — ненулевой код. Состояние и лог печатаем в любом случае.
state=
if ! $COMPOSE up -d --build; then
	state="docker compose up завершился с ошибкой"
fi

if [ -z "$state" ]; then
	step "Жду healthy у app (до $HEALTH_TIMEOUT с)"
	id=$($COMPOSE ps -q app)
	deadline=$((SECONDS + HEALTH_TIMEOUT))
	while :; do
		if [ -z "$id" ]; then
			state="контейнер app не найден"
			break
		fi
		state=$(docker inspect -f '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}' "$id")
		case $state in
		"running healthy" | *unhealthy* | exited* | dead* | restarting*) break ;;
		esac
		if [ "$SECONDS" -ge "$deadline" ]; then
			state="$state (не дождались за $HEALTH_TIMEOUT с)"
			break
		fi
		sleep 3
	done
fi
echo "app: $state"

step "Состояние"
$COMPOSE ps
step "Лог app (последние 30 строк)"
$COMPOSE logs --no-color --tail 30 app

if [ "$state" != "running healthy" ]; then
	exit 1
fi
echo
echo "Готово: $old → $(git rev-parse --short HEAD). Бэкап перед обновлением: $backup"
