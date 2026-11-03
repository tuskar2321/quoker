# quoker

Android-приложение: ссылка на TikTok или Instagram Reels → скачать в галерею или отправить через Telegram-бота в свой канал или чат.

- Спецификация: [`docs/`](docs/) — начать с [`docs/01-product.md`](docs/01-product.md) и [`docs/02-architecture.md`](docs/02-architecture.md)
- Правила для Claude Code: [`CLAUDE.md`](CLAUDE.md)
- Публичный API: [`api/openapi.yaml`](api/openapi.yaml)
- Контракт ядро ↔ воркер: [`proto/quoker/link/v1/link.proto`](proto/quoker/link/v1/link.proto)
- Макеты: [`docs/design/mockups/`](docs/design/mockups/)

## Компоненты

| Каталог | Что |
| --- | --- |
| `cmd/core`, `internal/core` | Ядро на сервере в РФ: API, БД, очередь, кэш |
| `cmd/worker`, `internal/worker` | Воркер за рубежом: yt-dlp, Telegram |
| `android/` | Клиент `app.quoker` |
| `deploy/` | Подготовка серверов, compose, Caddy, REALITY, бэкапы |
