# 10. Деплой

## Серверы

| Роль | Где | Ресурсы | ОС |
| --- | --- | --- | --- |
| Ядро | VPS в РФ, постоянный | 1 vCPU, 2 ГБ RAM, 10 ГБ диск | Ubuntu, Docker не установлен |
| Воркер | VPS в Нидерландах, заменяемый | 1 vCPU, 2 ГБ RAM, 10 ГБ диск | Ubuntu, Docker не установлен |

Оба сервера отделены от личного VPN владельца: свои IP, никаких VPN-портов. Код — GitHub, бэкапы — на ПК владельца.

## Домен и DNS

Один домен `.ru` у российского регистратора, DNS у регистратора (без проксирующих CDN). Имя нейтральное.

| Имя | Тип | Куда | Кто ходит |
| --- | --- | --- | --- |
| `api.<домен>` | A | IP ядра | Приложение, скачивание APK |
| `w.<домен>` | A | IP ядра | Воркер, только с клиентским сертификатом |

## Структура `deploy/`

```
deploy/
  bootstrap.sh                 # подготовка сервера: --role core|worker, --authorized-keys, --reality
  render-xray.sh               # рендер xray/config.json из шаблона и REALITY-значений из .env
  core/compose.yml
  core/Caddyfile
  core/.env.example
  core/xray/config.json.tmpl   # REALITY-вход на 8443, профиль fallback
  worker/compose.yml
  worker/.env.example
  worker/xray/config.json.tmpl # REALITY-клиент, профиль fallback
  pc/backup-pull.sh            # забирает бэкапы на ПК
  pc/new-worker-cert.sh        # выпуск сертификата воркера на ПК (CA-ключ не покидает ПК)
  pc/systemd/                  # сервис и таймер для ПК (Linux)
  pc/launchd/                  # агент launchd для ПК (macOS)
  pc/README.md                 # установка скриптов ПК
  images/                      # Dockerfile образов core и worker (контекст — корень репозитория)
```

Файлы на сервер: `deploy/<роль>/compose.yml` (+ `Caddyfile` для ядра), `.env` из `.env.example`, `xray/config.json.tmpl` и `deploy/render-xray.sh` при REALITY. Копирует владелец или workflow по SSH.

Раскладка на сервере, `/opt/app`:

- ядро: `compose.yml`, `Caddyfile`, `.env`, `certs/ca.crt`, `data/`, `cache/`, `releases/`, `backups/`, `xray/`;
- воркер: `compose.yml`, `.env`, `certs/{worker.crt,worker.key,ca.crt}`, `tmp/`, `xray/`.

## Образы

Все образы — из GHCR, `latest` не используется. Теги: версия из git-тега без префикса (`core-v1.2.0` → `1.2.0`) и `sha-<7 символов>`.

| Образ | Основа | Примечание |
| --- | --- | --- |
| `ghcr.io/tuskar2321/quoker-core:<CORE_TAG>` | distroless, uid 10001 | только бинарник `/usr/local/bin/core`, нет shell и curl |
| `ghcr.io/tuskar2321/quoker-worker:<WORKER_TAG>` | debian-slim, uid 10001 | Go-бинарник, ffmpeg, python3, yt-dlp (`YTDLP_VERSION`) |
| `ghcr.io/tuskar2321/quoker-caddy:<CADDY_TAG>` | зеркало официального `caddy` | `mirror-images`, раз в месяц |
| `ghcr.io/tuskar2321/quoker-xray:<XRAY_TAG>` | зеркало `ghcr.io/xtls/xray-core` | `mirror-images` |

Все образы — из GHCR: Docker Hub из РФ работает нестабильно, базовые образы тянутся на раннерах GitHub. Если ядро не сможет тянуть из GHCR — workflow делает `docker save | ssh deploy@core docker load`.

Сборка образов: контекст — корень репозитория; аргументы `VERSION` (уходит в `-X main.version`) и для воркера `YTDLP_VERSION` (по умолчанию фиксированный релиз, CI передаёт последний). Бинарники запускаются от `10001:10001`. Хостовые каталоги, которые пишут контейнеры, принадлежат `10001` (или группе `quoker`, см. ниже).

## `bootstrap.sh`

Запуск от root на новом сервере, Ubuntu 22.04+:

```sh
sudo ./bootstrap.sh --role core --authorized-keys /root/keys.pub [--reality]
sudo ./bootstrap.sh --role worker --authorized-keys /root/keys.pub
```

`--authorized-keys FILE` — публичные ключи владельца и CI, по одному в строке (без опций ключа). Без этого флага копируются ключи из `/root/.ssh/authorized_keys`, если у `deploy` их ещё нет. `--reality` на ядре открывает `8443/tcp`; без флага порт закрыт, и правило удаляется, если было добавлено раньше.

Шаги (идемпотентно, повторный запуск ничего не ломает):

1. `apt update && apt upgrade`, `unattended-upgrades` для security.
2. Пользователь `deploy` (группы `quoker` и `docker`), вход только по ключу: drop-in `/etc/ssh/sshd_config.d/10-quoker.conf` с `PermitRootLogin no` и `PasswordAuthentication no`. **Защита от блокировки:** скрипт отказывается отключать пароли и root, пока у `deploy` нет валидного публичного ключа; sshd проверяется `sshd -t` и `sshd -T`; перезагрузка, а не перезапуск, — текущие сессии не рвутся. Перед закрытием root-сессии проверьте вход `ssh deploy@<ip>` в новом окне. `fail2ban` с jail `sshd`.
3. Docker Engine + compose-плагин из apt-репозитория Docker; `deploy` в группе `docker`. Пакет `docker.io` из Ubuntu скрипт не допускает.
4. `/etc/docker/daemon.json`: `{"log-driver": "json-file", "log-opts": {"max-size": "10m", "max-file": "3"}}`.
5. Swap 1 ГБ (`/swapfile`, запись в `/etc/fstab`), если swap ещё нет.
6. `/opt/app` с владельцем `deploy` и подкаталоги, см. «Права».
7. ufw: ядро — 22, 80, 443 (+ 8443 с `--reality`); воркер — только 22. Порт SSH берётся из `sshd -T`.

**Важно про ufw и Docker:** опубликованные порты контейнеров (`ports:`) Docker пропускает через iptables мимо правил ufw. Снаружи доступно только то, что опубликовано в compose: на ядре `80`, `443` и (при fallback) `8443`. Внутренние порты ядра не публикуются вовсе.

### Права

Группа `quoker` с gid `10001` общая для `deploy` и контейнеров. Так `deploy` копирует файлы, а контейнер их читает без root.

| Каталог | Владелец | Режим | Кто пишет |
| --- | --- | --- | --- |
| `/opt/app` | `deploy:deploy` | 0755 | владелец |
| `certs/`, `xray/` | `deploy:quoker` | 2750 | владелец; контейнер читает через группу |
| `data/`, `cache/`, `tmp/` (воркер) | `10001:10001` | 0750 | контейнер |
| `backups/` | `10001:quoker` | 2750 | ядро; `deploy` читает через группу |
| `releases/` | `10001:quoker` | 2770 | ядро и `deploy` (загрузка APK) |

Ключ воркера `certs/worker.key` должен быть `0640` и группа `quoker`: загружать его через `umask 027 && cat > …` (см. `new-worker-cert.sh`). `render-xray.sh` создаёт `xray/config.json` с режимом `0640`.

## Контейнеры

Общие правила: `restart: unless-stopped`, `read_only: true`, `cap_drop: [ALL]`, `security_opt: no-new-privileges`, `pids_limit`, `tmpfs` для `/tmp`. Исключение — `caddy`: все capabilities сняты, кроме `NET_BIND_SERVICE` для портов 80/443.

| Сервер | Сервис | Образ | Тома | Лимит RAM | Проверка живости |
| --- | --- | --- | --- | --- | --- |
| Ядро | `caddy` | `quoker-caddy` (зеркало официального) | `caddy_data`, `./Caddyfile`, `./certs/ca.crt` | 128 МБ | — |
| Ядро | `core` | `quoker-core:<tag>` | `./data`, `./cache`, `./releases`, `./backups` | 512 МБ | `core healthcheck` |
| Ядро | `xray` | `quoker-xray` (профиль `fallback`) | `./xray` → `/etc/xray` | 128 МБ | — |
| Воркер | `worker` | `quoker-worker:<tag>` (Go + yt-dlp + ffmpeg) | `./certs` (ro), `./tmp` → `/tmp/quoker` | 1 ГБ | `worker healthcheck` |
| Воркер | `xray` | `quoker-xray` (профиль `fallback`) | `./xray` → `/etc/xray` | 128 МБ | — |

Проверки: образы без shell и curl, поэтому `healthcheck` вызывает бинарник: `core healthcheck` проверяет `127.0.0.1:<порт HTTP_ADDR>/healthz`, `worker healthcheck` — `HEALTH_ADDR` (по умолчанию `127.0.0.1:8090`). Оба выходят с 0 при успехе.

### Сети ядра

- `edge` — обычная сеть с выходом в интернет. В ней `caddy` (80/443) и `xray` (8443, только с профилем `fallback`).
- `backend` — `internal: true`: без выхода в интернет и без публикации портов, подсеть `172.30.0.0/24`. В ней `core`, `caddy` и `xray`. Порты `8080`, `8081`, `9090` доступны только из этой сети.

Ядро доверяет `X-Client-Cert-*` и `X-Forwarded-For` только от адресов из `TRUSTED_PROXIES`. По умолчанию это loopback и приватные сети; подсеть `172.30.0.0/24` входит в `172.16.0.0/12`. Значение `TRUSTED_PROXIES` в `.env.example` пустое, то есть действует значение по умолчанию.

### Caddyfile (ядро)

Файл `deploy/core/Caddyfile`. По сравнению с исходным описанием добавлено одно: блок `api` удаляет входящие `X-Client-Cert-*`, чтобы клиент не мог подставить заголовки сертификата на публичном API. Заголовки выставляются только в блоке `w.<домен>`.

```caddyfile
{
	email {$ACME_EMAIL}
}

api.{$DOMAIN} {
	encode zstd gzip
	request_header -X-Client-Cert-Fingerprint
	request_header -X-Client-Cert-CN
	request_header -X-Client-Cert-DER
	reverse_proxy core:8080
}

w.{$DOMAIN} {
	tls {
		client_auth {
			mode require_and_verify
			trust_pool file /etc/caddy/ca.crt
		}
	}
	@grpc protocol grpc
	reverse_proxy @grpc h2c://core:9090 {
		header_up X-Client-Cert-Fingerprint {http.request.tls.client.fingerprint}
		header_up X-Client-Cert-CN {http.request.tls.client.subject}
		header_up X-Client-Cert-DER {http.request.tls.client.certificate_der_base64}
	}
	reverse_proxy /internal/* core:8081 {
		header_up X-Client-Cert-Fingerprint {http.request.tls.client.fingerprint}
		header_up X-Client-Cert-CN {http.request.tls.client.subject}
		header_up X-Client-Cert-DER {http.request.tls.client.certificate_der_base64}
	}
}
```

Порты ядра: `8080` — публичный API, `8081` — приём файлов от воркеров и `/metrics`, `9090` — gRPC. Наружу открыт только Caddy. Запрос к 8081/9090 с адреса вне `TRUSTED_PROXIES` отклоняется.

### Переменные окружения

`core/.env` (шаблон — `deploy/core/.env.example`):

- Теги: `CORE_TAG` (версия релиза ядра), `CADDY_TAG`, `XRAY_TAG`.
- Домен: `DOMAIN`, `ACME_EMAIL`.
- Пути в контейнере (совпадают с каталогами на хосте): `DB_PATH=/data/quoker.db`, `CACHE_DIR=/cache`, `RELEASES_DIR=/releases`, `BACKUP_DIR=/backups`, `CACHE_MAX_BYTES=3221225472`.
- Адреса: `HTTP_ADDR=:8080`, `FILES_ADDR=:8081`, `GRPC_ADDR=:9090`.
- Пустые значения — по умолчанию: `UPLOAD_BASE` (`https://w.<DOMAIN>/internal/v1`), `PUBLIC_URL` (`https://api.<DOMAIN>`), `TRUSTED_PROXIES` (loopback и приватные сети).
- `LOG_LEVEL=info`, `TZ=Europe/Moscow`.
- REALITY: `REALITY_PRIVATE_KEY`, `REALITY_UUID`, `REALITY_SHORT_ID`, `REALITY_DEST`, `REALITY_SNI`. В контейнер `core` они не передаются, их читает только `render-xray.sh`.

Токен бота и cookies — не в `.env`, а в БД (`core settings set bot_token -`, `/cookies`).

`worker/.env` (шаблон — `deploy/worker/.env.example`):

- Теги: `WORKER_TAG` (версия релиза воркера), `XRAY_TAG`.
- Идентичность: `WORKER_ID` (совпадает с CN сертификата и с разрешённым id на ядре), `CORE_ADDR=w.<домен>:443`.
- Транспорт: `TRANSPORT=direct|reality` (по умолчанию `direct`). При `reality` обязателен `CORE_DIAL_ADDR=xray:10443`.
  - `CORE_DIAL_ADDR` — TCP-адрес, по которому воркер подключается и к gRPC-каналу, и к HTTPS-загрузкам на `UPLOAD_BASE`.
  - TLS ServerName и `Host` остаются от хоста `CORE_ADDR` (`w.<домен>`), поэтому mTLS заканчивается на Caddy без изменений.
  - Telegram и трафик к TikTok/Instagram не меняются: они всегда идут напрямую.
- Лимиты: `MAX_PARALLEL=2`, `TMP_CACHE_MAX_BYTES=1073741824`.
- Пути в контейнере: `TMP_DIR=/tmp/quoker` (на диске, в `./tmp`), `YTDLP_BIN=/usr/local/bin/yt-dlp`, `HEALTH_ADDR=127.0.0.1:8090`, `CLIENT_CERT`, `CLIENT_KEY`, `CA_CERT` фиксированы в compose (`/certs/...`).
- Прочее: `USER_AGENT` (пусто — встроенный мобильный UA), `LOG_LEVEL=info`, `TZ=UTC`.
- Домашний каталог: `HOME=/tmp`, потому что файловая система только для чтения; yt-dlp и python пишут кэш в `/tmp` (tmpfs 64 МБ).

## Сертификаты

| Сертификат | Кто выпускает | Где | Срок |
| --- | --- | --- | --- |
| `api.<домен>`, `w.<домен>` | Caddy, Let's Encrypt HTTP-01 | `caddy_data` | 90 дней, автопродление |
| Корневой CA | Владелец, `step` на ПК | Только ПК + офлайн-копия | 10 лет |
| Клиентский сертификат воркера | `deploy/pc/new-worker-cert.sh` (или `step certificate create … --profile leaf --ca … --not-after 8760h`) | Воркер `./certs` | 1 год |

Отзыв — удалением отпечатка в ядре (`/revoke_worker`), без CRL. Запасной вариант выпуска публичных сертификатов — DNS-01 через API регистратора.

## Запасной транспорт REALITY

Включается, если воркер не может держать стрим к ядру дольше 15 минут, а приложение с телефона ядро открывает. В обычной работе он выключен, но проверяется заранее.

1. **Ключи.** Сгенерировать свои значения, не общие с личным VPN:
   - `docker run --rm ghcr.io/xtls/xray-core:<XRAY_TAG> x25519` → приватный и публичный ключ;
   - `docker run --rm ghcr.io/xtls/xray-core:<XRAY_TAG> uuid` → UUID;
   - `openssl rand -hex 8` → `REALITY_SHORT_ID`.
2. **Ядро.** В `.env` заполнить `REALITY_*`. `REALITY_DEST` — настоящий сайт с TLS 1.3 и HTTP/2, `REALITY_SNI` — имя, которое этот сайт отдаёт. Затем:
   ```sh
   cd /opt/app && bash render-xray.sh core
   docker compose --profile fallback up -d xray
   sudo ufw allow 8443/tcp        # или bootstrap.sh --reality
   ```
   Xray слушает 8443, неавторизованным клиентам отвечает как `REALITY_DEST`, авторизованный трафик отправляет в `caddy:443`. Значит, mTLS воркера доходит до Caddy без изменений.
3. **Воркер.** В `.env`: `TRANSPORT=reality`, `CORE_DIAL_ADDR=xray:10443`, `REALITY_SERVER` (IP ядра), `REALITY_UUID`, `REALITY_PUBLIC_KEY` (публичный ключ), `REALITY_SHORT_ID`, `REALITY_SNI`, `CORE_HOST` (`w.<домен>`). Затем:
   ```sh
   cd /opt/app && bash render-xray.sh worker
   docker compose --profile fallback up -d
   ```
4. Проверить `docker compose --profile fallback logs xray` (принятые соединения) и `/status` в боте. Выключить: `docker compose --profile fallback stop xray` (ядро) и `TRANSPORT=direct` + `docker compose up -d worker` (воркер).

`render-xray.sh` читает `.env` как данные (не выполняет его), проверяет формат каждого значения, подставляет их в шаблон и проверяет результат через `jq`. Значения секретов в выводе не печатаются. Конфиг пишется в `xray/config.json` с правами `0640` и группой `quoker`.

Образ `xray` запускается командой `run -confdir /etc/xray`. Конфиг монтируется в `/etc/xray`, потому что путь по умолчанию в образе недоступен пользователю 10001.

## Бэкапы

1. **Ядро.** `core backup` делает `VACUUM INTO $BACKUP_DIR/quoker-<YYYYMMDD-HHMMSS>.db` (время UTC), через временный файл, оставляет 7 новейших и печатает путь. Сервер сам запускает это каждый день в 04:00 Europe/Moscow, если задан `BACKUP_DIR`. Перед каждой выкладкой CI вызывает `docker compose exec -T core core backup`.
   - Известное ограничение: снимки создаются с режимом `0600`. Пользователь `deploy`, от которого ходит `backup-pull`, их не прочитает, пока файлы не станут `0640` (нужна правка в Go-коде).
2. **ПК.** `deploy/pc/backup-pull.sh` по таймеру (при включении и раз в сутки, см. `deploy/pc/README.md`): rsync новых снимков, хранит 30. Папка на зашифрованном диске. `--verify` проверяет `PRAGMA integrity_check`.
3. **Проверка.** Раз в месяц на ПК: `quoker-backup-pull --verify` и восстановление снимка на тестовой копии.

## Мониторинг

Без отдельного Prometheus: сервисы отдают `/metrics` и `/healthz` во внутренней сети; алерты — через бота (`docs/06-telegram-bot.md`). Ядро и воркер следят друг за другом: воркер сообщает о недоступности ядра, ядро — об отсутствии воркеров.

## CI/CD (GitHub Actions)

| Workflow | Триггер | Что делает |
| --- | --- | --- |
| `ci` | push, PR | `buf lint`, `buf breaking` (против `master`), `go vet`, `golangci-lint`, `go test -race`, Android `lint` + `testDebugUnitTest` + `assembleDebug` |
| `release-core` | тег `core-v*` | Образ → GHCR; SSH на ядро: `core backup`, `docker compose pull core && up -d core`, ожидание `core healthcheck` |
| `release-worker` | тег `worker-v*` | Образ с последним yt-dlp → GHCR; SSH на воркер: `pull && up -d` |
| `ytdlp-refresh` | раз в неделю, вручную | Пересборка и выкладка воркера с новым yt-dlp |
| `release-android` | тег `app-v*` | Подписанный APK; SCP на ядро в `/opt/app/releases/quoker-<version_name>.apk`; регистрация по SSH: `docker compose exec -T core core release add <version_code> <version_name> /releases/quoker-<version_name>.apk - < changelog.txt` (проверяет, что это APK, считает `sha256`; занятые `version_code`/`version_name` отклоняет). Список — `core release list` |
| `mirror-images` | вручную, раз в месяц | Зеркалирование `caddy` и `xray` в GHCR |

Секреты GitHub: `CORE_SSH_HOST`, `CORE_SSH_KEY`, `CORE_SSH_KNOWN_HOSTS`, `WORKER_SSH_HOST`, `WORKER_SSH_KEY`, `WORKER_SSH_KNOWN_HOSTS`, `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD`. Переменная `ANDROID_API_BASE_URL` нужна для APK. Ключ CA в GitHub не хранится. Host-ключи серверов закрепляются через `*_SSH_KNOWN_HOSTS`. Образы тегируются версией и `sha`, `latest` не используется. Откат — повторный запуск workflow на предыдущем теге.

Деплой по SSH: в `/opt/app` копируются `compose.yml` (и `Caddyfile` для ядра), в `.env` выставляется `CORE_TAG`/`WORKER_TAG`, `docker login ghcr.io` с `GITHUB_TOKEN` через stdin, `docker compose pull <сервис> && docker compose up -d`, затем `docker logout ghcr.io`.

Соглашения:

- Теги релизов — `core-vX.Y.Z`, `worker-vX.Y.Z`, `app-vX.Y.Z`; релизы собираются только с `master`. Версия в `.env` сервера совпадает с версией тега без префикса.
- `ytdlp-refresh` собирает образ `<версия воркера>-yt<версия yt-dlp>` из исходников текущего `worker-v*` и выкладывает его; релизный тег и `sha`-тег не перезаписываются, откат — на обычный релизный тег.
- Деплой ядра: `core backup` → смена `CORE_TAG` → `pull` → `up -d --wait` → `core healthcheck`. Если сервис не стал здоровым, тег возвращается на прежний, job падает с логами. БД автоматически не откатывается — при необходимости восстановить из снимка (`docs/admin-manual.md`, «Бэкапы»).
- Окружения GitHub `core-production`, `worker-production`, `android-release` создаются автоматически; на них можно повесить обязательное подтверждение. Пушить теги релизов стоит разрешить только владельцу (tag ruleset).
- Known hosts: `ssh-keyscan -t ed25519 <host>`, отпечаток сверить с `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` на сервере до сохранения секрета.

## Ранбук: замена воркера

Цель — 30 минут от решения до приёма задач.

1. Убедиться, что проблема в IP воркера: ядро доступно с телефона, воркер не держит стрим ни по TLS, ни по REALITY.
2. Новый VPS (Ubuntu, ≥ 1 vCPU, 2 ГБ) в другой стране или подсети; `bootstrap.sh --role worker --authorized-keys …`.
3. На ПК: `deploy/pc/new-worker-cert.sh worker-<N>`, получить отпечаток.
4. В боте: `/allow_worker worker-<N> <fingerprint>`, `/revoke_worker <старый>`.
5. Положить сертификат, `ca.crt` и `.env` (файл ключа — `0640`, группа `quoker`), обновить `WORKER_SSH_HOST` в GitHub, запустить `release-worker` на текущем теге.
6. Проверить: воркер в `/status`, тестовые TikTok и Reels, тестовая отправка себе.
7. Instagram может попросить подтвердить вход служебного аккаунта с нового IP — подтвердить, обновить cookies (`/cookies`).
8. Удалить старый VPS и его секреты.

Раз в квартал — учения на временном VPS с замером времени.

## Первое развёртывание

1. Купить домен, A-записи `api` и `w` → IP ядра.
2. Сгенерировать ключи SSH владельца и CI; на обоих серверах `sudo ./bootstrap.sh --role core|worker --authorized-keys keys.pub`. В новом окне проверить `ssh deploy@<ip>`, и только потом закрыть root-сессию.
3. На ПК: CA (`step certificate create`), `deploy/pc/new-worker-cert.sh worker-1`, настроить `deploy/pc/backup-pull.sh` и таймер.
4. Секреты в GitHub (см. выше).
5. На ядре: скопировать `compose.yml`, `Caddyfile`, `.env` (из `.env.example`), `certs/ca.crt` (0644) в `/opt/app`; `docker login ghcr.io`; `docker compose up -d`. Затем `docker compose exec core core admin bootstrap` → инвайт администратора; `docker compose exec -T core core settings set bot_token -` (токен через stdin); `docker compose exec core core workers allow worker-1 <fingerprint>`.
6. На воркере: `.env`, `certs/` (`worker.crt`, `worker.key` 0640, `ca.crt`), `docker compose up -d`. Проверить heartbeat и выбор опрашивающего.
7. Проверка канала: стрим держится сутки, файл 50 МБ заливается целиком.
8. Заранее проверить REALITY-профиль и выключить.
9. Таймеры бэкапов на ядре (встроенный, 04:00 МСК) и на ПК, проверка восстановления.
10. Собрать и выложить первый APK, установить, войти по инвайту, привязать Telegram, отправить себе видео.
