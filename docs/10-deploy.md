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
  bootstrap.sh                 # подготовка сервера: --role core|worker
  core/compose.yml
  core/Caddyfile
  core/.env.example
  core/xray/config.json.tmpl   # REALITY-вход, профиль fallback
  worker/compose.yml
  worker/.env.example
  worker/xray/config.json.tmpl # REALITY-клиент, профиль fallback
  pc/backup-pull.sh            # забирает бэкапы на ПК
  pc/systemd/                  # unit + timer для ПК (Linux) и launchd plist (macOS)
```

## `bootstrap.sh`

1. `apt update && apt upgrade`, `unattended-upgrades` для security.
2. Пользователь `deploy`, вход только по ключу; `PermitRootLogin no`, `PasswordAuthentication no`; `fail2ban`.
3. Docker Engine + compose-плагин из apt-репозитория Docker; `deploy` в группе `docker`.
4. `/etc/docker/daemon.json`: `{"log-driver": "json-file", "log-opts": {"max-size": "10m", "max-file": "3"}}`.
5. Swap 1 ГБ.
6. `/opt/app` с владельцем `deploy`.
7. ufw: ядро — 22, 80, 443 (8443 только при включении REALITY); воркер — только 22.

Скрипт идемпотентный: повторный запуск ничего не ломает.

## Контейнеры

| Сервер | Сервис | Образ | Тома | Лимит RAM |
| --- | --- | --- | --- | --- |
| Ядро | `caddy` | `ghcr.io/tuskar2321/quoker-caddy` (зеркало официального) | `caddy_data`, `./Caddyfile`, `./certs/ca.crt` | 128 МБ |
| Ядро | `core` | `ghcr.io/tuskar2321/quoker-core:<tag>` | `./data`, `./cache`, `./releases`, `./backups` | 512 МБ |
| Ядро | `xray` | `ghcr.io/tuskar2321/quoker-xray` (профиль `fallback`) | `./xray` | 128 МБ |
| Воркер | `worker` | `ghcr.io/tuskar2321/quoker-worker:<tag>` (Go + yt-dlp + ffmpeg) | `./certs` (ro) | 1 ГБ |
| Воркер | `xray` | `ghcr.io/tuskar2321/quoker-xray` (профиль `fallback`) | `./xray` | 128 МБ |

Все образы — из GHCR: Docker Hub из РФ работает нестабильно, базовые образы тянутся на раннерах GitHub. Если ядро не сможет тянуть из GHCR — workflow делает `docker save | ssh deploy@core docker load`.

### Caddyfile (ядро)

```caddyfile
{
	email {$ACME_EMAIL}
}

api.{$DOMAIN} {
	encode zstd gzip
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

Порты ядра: `8080` — публичный API, `8081` — приём файлов от воркеров и `/metrics` (для Prometheus в той же docker-сети), `9090` — gRPC. Наружу открыт только Caddy. Ядро доверяет заголовкам `X-Client-Cert-*` и `X-Forwarded-For` только от адресов из `TRUSTED_PROXIES` (по умолчанию loopback и приватные сети — внутренняя сеть docker, где живёт Caddy). Запрос к 8081/9090 с другого адреса отклоняется, даже если порт случайно оказался доступен.

### Переменные окружения

`core/.env`: `DOMAIN`, `ACME_EMAIL`, `DB_PATH=/data/quoker.db`, `CACHE_DIR=/cache`, `CACHE_MAX_BYTES=3221225472`, `HTTP_ADDR=:8080`, `FILES_ADDR=:8081`, `GRPC_ADDR=:9090`, `UPLOAD_BASE` (по умолчанию `https://w.<DOMAIN>/internal/v1`; задаётся только для локальной разработки), `RELEASES_DIR=/releases`, `PUBLIC_URL` (по умолчанию `https://api.<DOMAIN>`, префикс `apk_url`), `BACKUP_DIR=/backups`, `TRUSTED_PROXIES` (CIDR через запятую, по умолчанию loopback и приватные сети), `LOG_LEVEL=info`, `TZ=Europe/Moscow`. Токен бота и cookies — не здесь, а в БД (`/status`, `/cookies`, CLI `core settings set bot_token`).

`worker/.env`: `CORE_ADDR=w.<домен>:443`, `TRANSPORT=direct|reality`, `CLIENT_CERT=/certs/worker.crt`, `CLIENT_KEY=/certs/worker.key`, `CA_CERT=/certs/ca.crt` (необязателен: добавляется к системным корням, а не заменяет их — сертификат `w.<домен>` выдан Let's Encrypt), `MAX_PARALLEL=2`, `TMP_DIR=/tmp/quoker`, `TMP_CACHE_MAX_BYTES=1073741824`, `YTDLP_BIN=yt-dlp`, `USER_AGENT=<мобильный UA, по умолчанию встроенный>`, `LOG_LEVEL=info`.

## Сертификаты

| Сертификат | Кто выпускает | Где | Срок |
| --- | --- | --- | --- |
| `api.<домен>`, `w.<домен>` | Caddy, Let's Encrypt HTTP-01 | `caddy_data` | 90 дней, автопродление |
| Корневой CA | Владелец, `step` на ПК | Только ПК + офлайн-копия | 10 лет |
| Клиентский сертификат воркера | Владелец, `step certificate create worker-N ... --profile leaf --ca ...` | Воркер `./certs` | 1 год |

Отзыв — удалением отпечатка в ядре (`/revoke_worker`), без CRL. Запасной вариант выпуска публичных сертификатов — DNS-01 через API регистратора.

## CI/CD (GitHub Actions)

| Workflow | Триггер | Что делает |
| --- | --- | --- |
| `ci` | push, PR | `buf lint`, `buf breaking` (против `main`), `go vet`, `golangci-lint`, `go test -race`, Android `lint` + `testDebugUnitTest` + `assembleDebug` |
| `release-core` | тег `core-v*` | Образ → GHCR; SSH на ядро: снимок БД, `docker compose pull core && up -d core`, проверка `/healthz` |
| `release-worker` | тег `worker-v*` | Образ с последним yt-dlp → GHCR; SSH на воркер: `pull && up -d` |
| `ytdlp-refresh` | раз в неделю, вручную | Пересборка и выкладка воркера с новым yt-dlp |
| `release-android` | тег `app-v*` | Подписанный APK; SCP на ядро в `/opt/app/releases/quoker-<version_name>.apk`; регистрация по SSH: `docker compose exec -T core core release add <version_code> <version_name> /releases/quoker-<version_name>.apk - < changelog.txt` (проверяет, что это APK, считает `sha256`; занятые `version_code`/`version_name` отклоняет). Список — `core release list` |
| `mirror-images` | вручную, раз в месяц | Зеркалирование `caddy` и `xray` в GHCR |

Секреты GitHub: `CORE_SSH_HOST`, `CORE_SSH_KEY`, `WORKER_SSH_HOST`, `WORKER_SSH_KEY`, `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD`. Ключ CA в GitHub не хранится. Образы тегируются версией и `sha`, `latest` не используется. Откат — повторный запуск workflow на предыдущем теге.

## Запасной транспорт REALITY

Включается, если воркер не может держать стрим к ядру дольше 15 минут, а приложение с телефона ядро открывает.

1. Ядро: `docker compose --profile fallback up -d xray`, `ufw allow 8443/tcp`. Xray слушает 8443, маскируется под крупный российский сайт, проксирует в `caddy:443` с SNI `w.<домен>`.
2. Воркер: `TRANSPORT=reality`, `docker compose --profile fallback up -d`. Xray-клиент поднимает локальный порт; `worker` подключается к нему вместо `w.<домен>:443`, mTLS и gRPC те же.
3. Ключи и UUID REALITY — свои, не общие с личным VPN.

## Бэкапы

1. Ядро, ежедневно в 04:00 МСК и перед каждой выкладкой: `VACUUM INTO /backups/quoker-<дата>.db`, хранить 7.
2. ПК: `deploy/pc/backup-pull.sh` по таймеру (при включении и раз в сутки) — `rsync` новых снимков, хранить 30; папка на зашифрованном диске.
3. Раз в месяц на ПК: `PRAGMA integrity_check` последнего снимка и контрольные запросы.

## Мониторинг

Без отдельного Prometheus: сервисы отдают `/metrics` и `/healthz` во внутренней сети; алерты — через бота (`docs/06-telegram-bot.md`). Ядро и воркер следят друг за другом: воркер сообщает о недоступности ядра, ядро — об отсутствии воркеров.

## Ранбук: замена воркера

Цель — 30 минут от решения до приёма задач.

1. Убедиться, что проблема в IP воркера: ядро доступно с телефона, воркер не держит стрим ни по TLS, ни по REALITY.
2. Новый VPS (Ubuntu, ≥ 1 vCPU, 2 ГБ) в другой стране или подсети; `bootstrap.sh --role worker`.
3. На ПК: новый клиентский сертификат `worker-<N>`.
4. В боте: `/allow_worker worker-<N> <fingerprint>`, `/revoke_worker <старый>`.
5. Положить сертификат и `.env`, обновить `WORKER_SSH_HOST` в GitHub, запустить `release-worker` на текущем теге.
6. Проверить: воркер в `/status`, тестовые TikTok и Reels, тестовая отправка себе.
7. Instagram может попросить подтвердить вход служебного аккаунта с нового IP — подтвердить, обновить cookies (`/cookies`).
8. Удалить старый VPS и его секреты.

Раз в квартал — учения на временном VPS с замером времени.

## Первое развёртывание

1. Купить домен, A-записи `api` и `w` → IP ядра.
2. `bootstrap.sh` на обоих серверах.
3. На ПК: `step` CA, сертификат `worker-1`.
4. Секреты в GitHub.
5. Выложить ядро; `core admin bootstrap` → инвайт администратора; `core settings set bot_token <...>`; `core workers allow worker-1 <fingerprint>`.
6. Выложить воркер; проверить heartbeat и выбор опрашивающего.
7. Проверка канала: стрим держится сутки, файл 50 МБ заливается целиком.
8. Заранее проверить REALITY-профиль и выключить.
9. Таймеры бэкапов на ядре и ПК, проверка восстановления.
10. Собрать и выложить первый APK, установить, войти по инвайту, привязать Telegram, отправить себе видео.
