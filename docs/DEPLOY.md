# Деплой Окуу на VPS

Пошаговая инструкция для первого запуска и обновлений. Сервер — Ubuntu 24.04 с Docker 29 и Compose v5,
файрвол ufw (открыты только 22, 80, 443). Код попадает на сервер через `git clone` из GitHub с read-only
deploy key. Все команды — на сервере, если не сказано иное.

## Что будет запущено

Три контейнера (`deploy/docker-compose.prod.yml`, проект `okuu-prod`):

| Контейнер | Что делает | Порты наружу |
| --- | --- | --- |
| `caddy` | HTTPS (сертификаты Let's Encrypt), статика приложения и лендинга, прокси `/api/*` и `/webhooks/*` в `app` | **80, 443** (единственные) |
| `app` | API, вебхук Telegram, фоновые задачи (начисления, напоминания); миграции БД — сам при старте | нет |
| `postgres` | PostgreSQL 16, данные — в томе `okuu-prod_pgdata` | нет |

У `postgres` и `app` портов нет специально: Docker публикует порты в обход ufw, и открытый порт был бы виден
из интернета, даже если ufw его «закрывает». База доступна только из внутренней сети, API — только через Caddy.

Фронтенд и лендинг собираются внутри образа `caddy` (`deploy/caddy.Dockerfile`): Node на сервере не нужен,
а образ — это одна версия сайта целиком. Адреса для лендинга берутся из доменов в `deploy/.env`.

Домены — два, у обоих DNS-запись на этот сервер:

- `APP_DOMAIN` — приложение, например `app.okuu.kg`;
- `LANDING_DOMAIN` — лендинг, например `okuu.kg`.

## 1. Сервер

Под root (или через `sudo`):

```sh
# Пользователь для сервиса: в группе docker, без пароля (вход — по SSH-ключу)
adduser --disabled-password --gecos "" okuu
usermod -aG docker okuu

# Каталоги: код, бэкапы, лог бэкапов
mkdir -p /opt/okuu /var/backups/okuu /var/log/okuu
chown okuu: /opt/okuu /var/backups/okuu /var/log/okuu
chmod 700 /var/backups/okuu

# Файрвол: только SSH, HTTP и HTTPS (443 — и tcp, и udp для HTTP/3)
ufw allow OpenSSH && ufw allow 80/tcp && ufw allow 443 && ufw enable
ufw status
```

Сборка образов на сервере (Go и фронтенд) берёт до 1,5–2 ГБ памяти. Если у VPS 2 ГБ и меньше — добавьте swap:

```sh
fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile
echo '/swapfile none swap sw 0 0' >> /etc/fstab
```

**DNS.** Записи `A` (и `AAAA`, если у сервера есть IPv6) для обоих доменов → IP сервера. Проверка:
`dig +short app.okuu.kg` и `dig +short okuu.kg` возвращают IP сервера. Без этого Caddy не получит сертификаты.

## 2. Код (deploy key)

Под пользователем `okuu` (`su - okuu`):

```sh
ssh-keygen -t ed25519 -f ~/.ssh/okuu_deploy -N "" -C "okuu-deploy@$(hostname)"
cat ~/.ssh/okuu_deploy.pub
```

В GitHub: репозиторий → **Settings → Deploy keys → Add deploy key**, вставьте ключ, галочку
«Allow write access» **не** ставьте (только чтение).

```sh
cat >> ~/.ssh/config <<'EOF'
Host github-okuu
    HostName github.com
    User git
    IdentityFile ~/.ssh/okuu_deploy
    IdentitiesOnly yes
EOF
chmod 600 ~/.ssh/config

git clone git@github-okuu:<владелец>/<репозиторий>.git /opt/okuu
cd /opt/okuu && git log -1 --oneline
```

Скрипты бэкапа запускаются через `bash deploy/backup.sh` — права на исполнение не нужны (`chmod` дал бы
локальное изменение, и `git pull` мог бы отказаться обновлять).

Чтобы не набирать длинную команду compose, добавьте сокращение (дальше в инструкции — `dc`):

```sh
echo "alias dc='docker compose -f /opt/okuu/deploy/docker-compose.prod.yml'" >> ~/.bashrc && . ~/.bashrc
```

## 3. Настройки (`deploy/.env`)

```sh
cd /opt/okuu
cp deploy/.env.prod.example deploy/.env
chmod 600 deploy/.env
```

Секреты генерируйте, а не придумывайте. Эти команды впишут случайные значения прямо в файл (на экран не
выводятся):

```sh
cd /opt/okuu/deploy
sed -i "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=$(openssl rand -hex 24)|" .env
sed -i "s|^ENCRYPTION_KEY=.*|ENCRYPTION_KEY=$(openssl rand -base64 32)|" .env
sed -i "s|^TELEGRAM_WEBHOOK_SECRET=.*|TELEGRAM_WEBHOOK_SECRET=$(openssl rand -hex 32)|" .env
```

Остальное впишите сами (`nano deploy/.env`):

| Переменная | Что вписать |
| --- | --- |
| `APP_DOMAIN` | домен приложения без `https://`, например `app.okuu.kg` |
| `LANDING_DOMAIN` | домен лендинга, например `okuu.kg` |
| `ACME_EMAIL` | e-mail для писем Let's Encrypt |
| `TELEGRAM_BOT_TOKEN` | токен **боевого** бота от @BotFather (не того, что для разработки) |
| `TELEGRAM_BOT_USERNAME` | имя бота без `@` |
| `ALERT_TELEGRAM_CHAT_ID` | id служебной группы для алертов (см. ниже); можно оставить пустым — алерты будут только в логе |
| `SUPPORT_WHATSAPP` | номер поддержки, только цифры с кодом страны: `996700123456` |

Уже заполнены и менять не нужно: `POSTGRES_DB`, `POSTGRES_USER`, `BOT_LANGUAGES=ru`, `REGISTRATION_MODE=invite`,
`ONLINE_PAYMENTS_ENABLED=false`, `FINIK_ENABLED=false`, `BACKUP_DIR`, `KEEP_DAYS=14`.
`APP_ENV=prod`, `TELEGRAM_MODE=webhook` и `APP_ORIGIN=https://<APP_DOMAIN>` задаёт сам compose-файл.

**`ENCRYPTION_KEY` сохраните копию вне сервера** (в менеджере паролей): без него не расшифровать сохранённые
ключи провайдера оплаты после переезда или восстановления.

**Id служебного чата** — до первого запуска (потом сервер поставит вебхук, и `getUpdates` перестанет отвечать):
добавьте бота в служебную группу, напишите в ней любое сообщение и выполните

```sh
TOKEN=$(grep '^TELEGRAM_BOT_TOKEN=' /opt/okuu/deploy/.env | cut -d= -f2-)
curl -s "https://api.telegram.org/bot$TOKEN/getUpdates" | grep -o '"chat":{"id":-[0-9]*' | sort -u
unset TOKEN
```

Число после `"id":` (отрицательное) — в `ALERT_TELEGRAM_CHAT_ID`.

Проверка, что все обязательные переменные на месте (секреты не печатаются):

```sh
dc config --quiet && echo "deploy/.env в порядке"
```

## 4. Первый запуск

```sh
cd /opt/okuu
dc up -d --build          # сборка образов — несколько минут
dc ps                     # postgres и app — (healthy), caddy — Up
dc logs app | tail -30
```

В логе `app` (JSON, по строке на событие) должны быть:

- `миграция применена` по каждому файлу и `схема БД актуальна` — **миграции применяются сами при старте**;
- `регистрация центров` с `"mode":"invite"`;
- `онлайн-оплата выключена`;
- `telegram: бот запущен (webhook)`;
- `HTTP сервер запущен` с `"env":"prod"`.

Если конфигурация неполная, `app` не стартует и перезапускается по кругу. Причины — в `dc logs app`: «Ошибка
конфигурации — сервер не запущен» и список («TELEGRAM_BOT_TOKEN не задан», «ENCRYPTION_KEY: нужно минимум 32
байт» и т. п.). Исправьте `deploy/.env` и снова `dc up -d`.

Сертификаты Caddy получает сам в первые секунды: `dc logs caddy | grep -i "certificate obtained"` — по строке
на каждый домен.

### Миграции

Отдельно запускать не нужно: `app` применяет их при каждом старте, а `/readyz` (healthcheck контейнера)
не станет «здоров», пока есть неприменённые. Посмотреть состояние:

```sh
dc exec app /server migrate status     # номер, applied, дата, файл
```

Миграции не откатываются автоматически — см. «Откат».

## 5. Первый центр и суперадмин

Регистрация центров закрыта (`REGISTRATION_MODE=invite`): центр регистрируется только по одноразовой ссылке.
Первую ссылку выдаёт консоль:

```sh
dc exec app /server admin registration-link --note="мой центр"
# Ссылка регистрации центра (мой центр), одноразовая, действует до …:
# https://app.okuu.kg/register/<токен>
```

Откройте ссылку и зарегистрируйте свой центр (телефон владельца — ваш). Затем выдайте себе права суперадмина:

```sh
dc exec app /server admin grant --phone=0700123456
# Имя Фамилия — суперадмин. После входа в приложении появится раздел «Панель платформы» (/admin).
```

После повторного входа в меню аккаунта появится «Панель платформы»: ссылки регистрации для следующих центров,
статусы, тарифы. Снять права — `admin revoke --phone=…`. Через API права не выдаются — только этой командой.

Время в выводе консольных команд — UTC (на 6 часов меньше бишкекского).

## 6. Демо-центр

Для показа клиентам — отдельный центр с вымышленными данными (40 учеников, должники, история оплат):

```sh
dc exec app /server demo seed --phone=0555099001 --password='<отдельный пароль для демо>'
```

Вход — этим телефоном и паролем. Пересоздать с нуля после показа: `dc exec app /server demo reset`.
Telegram и напоминания для демо не работают. Пароль остаётся в истории shell — не используйте рабочий.
Подробнее — README, «Демо для показа».

## 7. Вебхук Telegram

Ставится сам при каждом старте `app`: адрес `https://<APP_DOMAIN>/webhooks/telegram/<TELEGRAM_WEBHOOK_SECRET>`,
плюс секрет в заголовке, который Telegram присылает с каждым запросом. Меню команд бота — тоже само.

Проверка (токен не попадёт в историю shell):

```sh
TOKEN=$(grep '^TELEGRAM_BOT_TOKEN=' /opt/okuu/deploy/.env | cut -d= -f2-)
curl -s "https://api.telegram.org/bot$TOKEN/getWebhookInfo" | sed -E 's#(/webhooks/telegram/)[^"]+#\1…#'
unset TOKEN
```

Ожидается `"url":"https://app.okuu.kg/webhooks/telegram/…"`, `"pending_update_count":0` и **нет**
`last_error_message`. Затем напишите боту `/start` — он ответит подсказкой о подключении по ссылке.

Если в логе `app` — «не удалось установить вебхук» (и алерт в служебном чате): проверьте токен, DNS и
сертификат (`dc logs caddy`), затем `dc restart app` — вебхук поставится заново.

## 8. Проверка HTTPS и закрытых портов

```sh
D=app.okuu.kg; L=okuu.kg     # ваши домены

curl -sI https://$D/ | grep -iE "^HTTP|strict-transport"         # HTTP/2 200 и HSTS
curl -s https://$D/api/v1/config; echo                            # {"online_payments":false}
curl -sI http://$D/ | grep -iE "^HTTP|^location"                  # 308 → https://
curl -so /dev/null -w "%{http_code}\n" https://$D/healthz         # 404 — служебное наружу закрыто
curl -sI https://$L/ | grep -iE "^HTTP|content-security"          # лендинг: 200 и CSP
curl -s https://$L/ | grep -o 'href="https://[^"]*/login"'        # кнопка «Войти» ведёт на https://$D/login
echo | openssl s_client -connect $D:443 -servername $D 2>/dev/null | openssl x509 -noout -issuer -enddate
                                                                  # issuer — Let's Encrypt
```

Что наружу открыто только 22, 80 и 443 — на сервере:

```sh
sudo ss -tlnp | grep -E ':(5432|8080)\s' || echo "5432 и 8080 на сервере не слушаются"
sudo ss -tlnp                                  # снаружи (0.0.0.0 / [::]) — только 22, 80, 443
dc ps --format '{{.Service}}\t{{.Ports}}'      # порты только у caddy: 0.0.0.0:80, 0.0.0.0:443
```

И с другого компьютера: `nc -zv -w 3 <IP сервера> 5432` и `… 8080` — `Connection refused` или таймаут.

В браузере: откройте `https://<APP_DOMAIN>`, войдите, проверьте замок в адресной строке.

## 9. Бэкапы

`deploy/backup.sh` — `pg_dump | gzip` в `BACKUP_DIR` (`/var/backups/okuu`), проверяет, что архив целый и дамп
дописан до конца, и удаляет архивы старше `KEEP_DAYS` (14) дней. Ежедневный запуск в 03:00 — cron
(под root):

```sh
install -m 644 /opt/okuu/deploy/okuu-backup.cron /etc/cron.d/okuu-backup
```

Первый бэкап — вручную, сразу (под `okuu`):

```sh
bash /opt/okuu/deploy/backup.sh            # печатает путь: /var/backups/okuu/okuu-20261105-030000.sql.gz
ls -lh /var/backups/okuu
```

Дальше смотрите лог: `tail /var/log/okuu/backup.log` — по строке-пути на каждый день, ошибки — там же.

**Проверка восстановления** — сразу после первого бэкапа и потом раз в месяц:

```sh
bash /opt/okuu/deploy/restore.sh /var/backups/okuu/okuu-<дата>.sql.gz
```

Скрипт накатывает архив во временную базу `okuu_restore_check`, печатает версию схемы и число строк
(центры, пользователи, ученики, операции, уведомления, журнал) и удаляет её. Рабочая база не трогается.

**Копия вне сервера.** Архивы на том же диске не спасут при потере сервера. Раз в сутки копируйте
`/var/backups/okuu` в другое место — например, `rclone sync /var/backups/okuu <remote>:okuu-backups` второй
строкой в `/etc/cron.d/okuu-backup` (после настройки `rclone config`) или скачивайте на свой компьютер:
`scp okuu@<сервер>:/var/backups/okuu/okuu-*.sql.gz .`

**Восстановление после аварии** (данные будут заменены данными из архива):

```sh
cd /opt/okuu
dc stop app caddy                                        # никто не пишет в базу
CONFIRM=yes bash deploy/restore.sh /var/backups/okuu/okuu-<дата>.sql.gz okuu
dc start app caddy && dc ps                              # app — healthy
```

Без `CONFIRM=yes` скрипт в рабочую базу не пишет. На новом сервере: шаги 1–4, затем эта команда.
Нужен тот же `ENCRYPTION_KEY`, что был на старом сервере.

## 10. Обновление версии

```sh
cd /opt/okuu
git fetch && git log --oneline HEAD..origin/main          # что придёт
bash deploy/backup.sh                                     # бэкап перед обновлением
git log -1 --oneline                                      # запишите текущий коммит — для отката
docker tag okuu-app:latest okuu-app:prev                  # текущие образы — на случай отката
docker tag okuu-web:latest okuu-web:prev
git pull --ff-only
dc up -d --build                                          # пересборка; новые миграции применятся при старте
dc ps                                                     # app — healthy
dc logs app | tail -20
```

Во время перезапуска `app` (несколько секунд) API недоступен: текущие запросы и фоновые задачи сервер
доделывает (до 20 с), новые запросы получат ошибку. Сайт откроется новой версией при следующем открытии
страницы: `index.html` не кэшируется, файлы с хэшем в имени кэшируются навсегда.

Если менялся только `deploy/.env` — достаточно `dc up -d` (без `--build`).

## 11. Откат

**Быстро — на прежние образы** (сохранены как `:prev` перед обновлением):

```sh
cd /opt/okuu
docker tag okuu-app:prev okuu-app:latest
docker tag okuu-web:prev okuu-web:latest
dc up -d --no-build
dc ps
git checkout <прежний коммит>        # код на диске — как у работающей версии (для следующей сборки)
```

Вернуться на актуальную ветку при следующем обновлении: `git checkout main && git pull --ff-only`.

**Если образов `:prev` нет** — `git checkout <прежний коммит> && dc up -d --build`.

**Миграции назад не откатываются.** Старая версия работает с новой схемой, если миграции только добавляли
таблицы и колонки (так их и пишем). Если обновление испортило данные или схему — восстановите бэкап, сделанный
перед обновлением (раздел 9, «Восстановление после аварии»), и запустите прежние образы. Данные, внесённые
после обновления, при этом потеряются.

## Полезные команды

```sh
dc ps                                   # состояние контейнеров
dc logs -f app                          # лог API (Ctrl+C — выйти)
dc logs --since 1h caddy                # лог Caddy за час
dc restart app                          # перезапуск API
dc exec postgres psql -U okuu okuu      # консоль базы (осторожно: это рабочие данные)
dc exec app /server user reset-link --phone=0700123456   # ссылка сброса пароля (README, «Сброс пароля»)
df -h / && du -sh /var/backups/okuu     # место на диске
```

Контейнеры перезапускаются сами (`restart: unless-stopped`) — и после сбоя, и после перезагрузки сервера.
Логи контейнеров ротируются (до 5 файлов по 20 МБ на контейнер).

## Переменные `deploy/.env`

| Переменная | Обязательна | Значение |
| --- | --- | --- |
| `APP_DOMAIN` | да | домен приложения |
| `LANDING_DOMAIN` | да | домен лендинга |
| `ACME_EMAIL` | да | e-mail для Let's Encrypt |
| `POSTGRES_DB`, `POSTGRES_USER` | — | `okuu` |
| `POSTGRES_PASSWORD` | да | `openssl rand -hex 24` |
| `ENCRYPTION_KEY` | да | `openssl rand -base64 32` (копию — вне сервера) |
| `TELEGRAM_BOT_TOKEN` | да | токен боевого бота от @BotFather |
| `TELEGRAM_BOT_USERNAME` | да | имя бота без `@` |
| `TELEGRAM_WEBHOOK_SECRET` | да | `openssl rand -hex 32` |
| `ALERT_TELEGRAM_CHAT_ID` | нет | id служебной группы (отрицательное число) |
| `BOT_LANGUAGES` | — | `ru` (`ru,ky` — после проверки кыргызских текстов) |
| `REGISTRATION_MODE` | — | `invite` |
| `SUPPORT_WHATSAPP` | желательно | номер поддержки, цифры с кодом страны |
| `ONLINE_PAYMENTS_ENABLED`, `FINIK_ENABLED` | — | `false` до подключения Finik |
| `FINIK_API_KEY` | нет | пусто до Finik |
| `BACKUP_DIR`, `KEEP_DAYS` | — | `/var/backups/okuu`, `14` |

Задаёт `deploy/docker-compose.prod.yml`, в `.env` не пишите: `APP_ENV=prod`, `TELEGRAM_MODE=webhook`,
`APP_ORIGIN=https://<APP_DOMAIN>`, `HTTP_ADDR`, `DATABASE_URL`, `TRUSTED_PROXIES`.
