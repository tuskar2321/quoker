# 03. Публичный API ядра

Базовый URL: `https://api.<домен>`. Формальная схема — `api/openapi.yaml`; при расхождении правится и то и другое.

## Общие правила

- JSON, UTF-8, `snake_case`. Время — RFC 3339 в UTC.
- Аутентификация: `Authorization: Bearer <session_token>`. Без токена доступны только `POST /v1/auth/register` и `GET /v1/app/version`.
- Заголовки клиента: `X-Quoker-Version-Code: <int>`, `X-Device-Id: <uuid>`. Ядро отклоняет версии ниже `min_supported_version_code` с `426 upgrade_required`.
- Идемпотентность: `POST /v1/sends` принимает `Idempotency-Key` (UUID); повтор с тем же ключом возвращает ту же отправку.
- Пагинация курсором: `?cursor=<opaque>&limit=<1..50>`, ответ `{ "items": [...], "next_cursor": "..." | null }`.

## Ошибки

```json
{ "error": { "code": "private_video", "message": "Видео приватное или удалено", "retryable": false } }
```

`message` — готовый русский текст для показа; клиент может заменить его своим по `code`.

| Код | HTTP | Когда |
| --- | --- | --- |
| `unauthorized` | 401 | Нет или недействительна сессия |
| `forbidden` | 403 | Нет прав (не админ, чужая цель) |
| `user_blocked` | 403 | Пользователь заблокирован |
| `upgrade_required` | 426 | Версия приложения ниже минимальной |
| `invalid_invite` | 400 | Инвайт не найден, истёк или использован |
| `telegram_not_linked` | 409 | Действие требует привязанного Telegram |
| `link_token_expired` | 410 | Токен привязки истёк |
| `unsupported_url` | 422 | Ссылка не TikTok и не Instagram или неизвестный формат |
| `unsupported_content` | 422 | Фото, карусель, сторис, эфир |
| `private_video` | 404 | Приватное или удалённое видео |
| `not_found` | 404 | Нет такого видео, задачи, цели |
| `file_not_ready` | 409 | Файл ещё не подготовлен (`/file` до `prepare`) |
| `too_large` | 422 | Нет формата до 50 МБ для отправки |
| `target_inactive` | 409 | Цель неактивна (бот удалён, нет прав) |
| `target_forbidden` | 403 | Пользователь потерял права в чате |
| `rate_limited` | 429 | Превышен лимит; `Retry-After` в секундах |
| `worker_unavailable` | 503 | Нет подключённых воркеров дольше 10 минут |
| `upstream_error` | 502 | Платформа или Telegram ответили ошибкой; `retryable: true` |
| `internal` | 500 | Прочее |

## Модели

```jsonc
// Video
{
  "id": "tiktok:7301234567890123456",
  "platform": "tiktok",              // tiktok | instagram
  "source_url": "https://vm.tiktok.com/ZM...",
  "canonical_url": "https://www.tiktok.com/@author/video/7301234567890123456",
  "author": "author",
  "caption": "Описание автора",
  "duration_s": 27,
  "size_bytes": 14680064,             // лучший формат; null, если неизвестен
  "width": 1080, "height": 1920,
  "no_watermark": true,               // только для tiktok
  "thumbnail_url": "/v1/videos/tiktok:7301.../thumbnail",
  "file": { "status": "none" }        // none | preparing | ready | failed
}

// Target
{
  "id": 42,
  "title": "Друзья",
  "type": "group",                    // group | channel | private
  "status": "active",                 // active | inactive
  "inactive_reason": null,            // bot_removed | bot_not_admin | user_left | user_not_admin
  "hidden": false,
  "last_used_at": "2026-11-02T12:40:00Z"
}

// Send
{
  "id": 1001, "video_id": "tiktok:...", "target_id": 42,
  "status": "sent",                   // queued | sending | sent | failed
  "error": null,
  "created_at": "...", "updated_at": "..."
}

// Task
{ "id": "t_01J...", "kind": "resolve", "status": "running", "progress": 40, "video": null, "error": null }
```

## Эндпоинты

### Аутентификация и аккаунт

| Метод | Путь | Тело / параметры | Ответ |
| --- | --- | --- | --- |
| POST | `/v1/auth/register` | `{invite_code, device_id, device_name}` | `201 {session_token, user}` |
| POST | `/v1/auth/logout` | — | `204`, сессия отозвана |
| GET | `/v1/me` | — | `{id, role, telegram: {linked, username, first_name}}` |
| POST | `/v1/auth/telegram/link` | — | `{deep_link, expires_at}` — `https://t.me/<bot>?start=link_<token>` |
| GET | `/v1/auth/telegram/status` | — | `{status: pending\|linked\|expired, username}` |
| DELETE | `/v1/auth/telegram` | — | `204`, цели пользователя деактивируются |

### Видео и задачи

| Метод | Путь | Тело / параметры | Ответ |
| --- | --- | --- | --- |
| POST | `/v1/videos/resolve` | `{url}` | `200 {video}` или `202 {task_id}` (ядро ждёт до 15 с) |
| GET | `/v1/tasks/{task_id}` | — | `{task}`; для `resolve` при `done` содержит `video` |
| GET | `/v1/videos/{id}` | — | `{video}` |
| GET | `/v1/videos/{id}/thumbnail` | — | `image/jpeg` |
| POST | `/v1/videos/{id}/prepare` | — | `200 {file: {status: ready, size_bytes}}` или `202 {task_id}` |
| GET | `/v1/videos/{id}/file` | `Range` | `200/206 video/mp4`; `409 file_not_ready` |
| POST | `/v1/videos/{id}/downloaded` | — | `204`, запись в историю |

### Цели и отправки

| Метод | Путь | Тело / параметры | Ответ |
| --- | --- | --- | --- |
| GET | `/v1/targets` | `?include_hidden=true` | `{items: [target]}`; сортировка: последняя использованная, затем по названию |
| PATCH | `/v1/targets/{id}` | `{hidden}` | `{target}` |
| POST | `/v1/targets/refresh` | — | `202`; ядро перепроверяет права по всем целям пользователя |
| POST | `/v1/sends` | `{video_id, target_id, caption, sign_as_user}` + `Idempotency-Key` | `202 {send}` |
| GET | `/v1/sends/{id}` | — | `{send}` |

`caption` — строка до 1024 символов или `null` (ядро подставит ссылку на оригинал); пустая строка — без подписи. `sign_as_user` учитывается только для групп.

### История

| Метод | Путь | Ответ |
| --- | --- | --- |
| GET | `/v1/history` | `{items: [{id, video, actions: [{type: resolved\|downloaded\|sent\|send_failed, target_title, at}]}], next_cursor}` — по одной записи на видео, действия внутри |

### Приложение

| Метод | Путь | Ответ |
| --- | --- | --- |
| GET | `/v1/app/version` | `{version_code, version_name, apk_url, sha256, min_supported_version_code, changelog}` |
| GET | `/download/quoker-<version_name>.apk` | APK (без авторизации, ссылка из `version`) |

### Администрирование (роль `admin`)

| Метод | Путь | Тело | Ответ |
| --- | --- | --- | --- |
| GET | `/v1/admin/status` | — | `{workers: [...], queue: {queued, running}, errors_24h: {tiktok, instagram, telegram}, disk: {used_bytes, total_bytes, cache_bytes}, versions}` |
| GET | `/v1/admin/invites` | — | `{items}` |
| POST | `/v1/admin/invites` | `{role: user\|admin, expires_in_days}` | `201 {code, expires_at}` — код показывается один раз |
| DELETE | `/v1/admin/invites/{id}` | — | `204` |
| GET | `/v1/admin/users` | — | `{items: [{id, role, status, telegram_username, created_at, last_seen_at}]}` |
| POST | `/v1/admin/users/{id}/block` | — | `204`, сессии отозваны |
| POST | `/v1/admin/users/{id}/unblock` | — | `204` |
| PUT | `/v1/admin/instagram-cookies` | `text/plain` (Netscape cookies.txt) | `204` |
| GET | `/v1/admin/workers` | — | `{items: [{id, fingerprint, status, connected, version, ytdlp_version, last_seen_at, is_poller}]}` |
| POST | `/v1/admin/workers` | `{id, fingerprint}` | `201` — разрешить |
| DELETE | `/v1/admin/workers/{id}` | — | `204` — отозвать, стрим рвётся |

## Лимиты

| Что | Лимит |
| --- | --- |
| `POST /v1/videos/resolve` | 30 в час на пользователя |
| `POST /v1/sends` | 30 в час на пользователя |
| `POST /v1/auth/register` | 5 в час на IP |
| Скачивание файла | без лимита по количеству, 2 параллельных на сессию |
