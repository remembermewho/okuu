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
# COMPOSE — команда compose. deploy/.env скрипт не читает: его читает только compose.
set -euo pipefail

cd "$(dirname "$0")/.."
root=$(pwd)

COMPOSE=${COMPOSE:-docker compose -f $root/deploy/docker-compose.prod.yml}
HEALTH_TIMEOUT=${HEALTH_TIMEOUT:-120}
# Имена образов — как в deploy/docker-compose.prod.yml (image: у app и caddy).
APP_IMAGE=okuu-app:latest
WEB_IMAGE=okuu-web:latest
if ! [[ $HEALTH_TIMEOUT =~ ^[1-9][0-9]*$ ]]; then
	echo "HEALTH_TIMEOUT: целое число секунд, получено «$HEALTH_TIMEOUT»" >&2
	exit 2
fi

prev_of() { echo "${1%:*}:prev"; }
step() { printf '\n== %s\n' "$*"; }

# Тихих выходов нет: любая неожиданная ошибка (set -e) называет строку и команду, а on_exit ниже —
# что успело измениться и как вернуть прежнюю версию.
on_error() {
	echo >&2
	echo "update.sh: ошибка в строке $1: $2 (код $3)" >&2
}
trap 'on_error "$LINENO" "$BASH_COMMAND" "$?"' ERR

old=
new=
backup=
pulled=
declare -a prev_images=()
on_exit() {
	local status=$?
	[ "$status" -eq 0 ] && return
	echo >&2
	if [ -z "$pulled" ]; then
		{
			echo "Обновление остановлено до git pull: код, контейнеры и база не менялись${old:+, работает $old}."
			if [ -n "$backup" ]; then
				echo "Бэкап перед обновлением сделан: $backup"
			fi
			if [ "${#prev_images[@]}" -gt 0 ]; then
				echo "Образы помечены :prev (${prev_images[*]}) — это ничему не мешает, откатывать нечего."
			fi
		} >&2
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
trap on_exit EXIT

# --- Git: на ветке, без локальных правок, обновление — перемотка вперёд.
if ! branch=$(git symbolic-ref --quiet --short HEAD); then
	echo "Код не на ветке (git checkout <коммит> после отката?). Вернитесь на ветку: git checkout main" >&2
	exit 1
fi
if ! upstream=$(git rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null); then
	echo "У ветки $branch нет удалённой ветки для обновления (git branch -u origin/$branch)" >&2
	exit 1
fi
# Вывод git — в переменные, а не прямо в [ ]: так ошибка git не теряется (set -e и trap ERR видят её).
changed=$(git status --porcelain --untracked-files=no)
if [ -n "$changed" ]; then
	echo "В $root изменены файлы из репозитория — git pull может не пройти. Посмотрите: git status, git diff" >&2
	exit 1
fi

step "Новые коммиты ($upstream)"
git fetch --quiet
old=$(git rev-parse --short HEAD)
incoming=$(git rev-list --count "HEAD..$upstream")
if [ "$incoming" -eq 0 ]; then
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
	answer=
	read -r -p "Обновить $old → $new? [y/N] " answer || true # Ctrl-D — то же, что «нет»
	if ! [[ $answer =~ ^[yYдД] ]]; then
		echo
		echo "Отменено, ничего не изменено."
		exit 0
	fi
fi

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
	# Ошибки docker здесь не обрываем: ниже всё равно печатаем состояние, лог и команды отката.
	id=$($COMPOSE ps -q app) || id=
	deadline=$((SECONDS + HEALTH_TIMEOUT))
	while :; do
		if [ -z "$id" ]; then
			state="контейнер app не найден"
			break
		fi
		if ! state=$(docker inspect -f '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}' "$id"); then
			state="docker inspect не нашёл контейнер app"
			break
		fi
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
$COMPOSE ps || echo "(docker compose ps не сработал)"
step "Лог app (последние 30 строк)"
$COMPOSE logs --no-color --tail 30 app || echo "(docker compose logs не сработал)"

if [ "$state" != "running healthy" ]; then
	echo >&2
	echo "app не в состоянии «running healthy»: $state" >&2
	exit 1
fi
echo
echo "Готово: $old → $(git rev-parse --short HEAD). Бэкап перед обновлением: $backup"
