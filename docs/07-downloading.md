# 07. Скачивание (воркер)

## Нормализация ссылок (`internal/shared/urls`, ядро)

1. Из текста взять первый `https?://` URL поддерживаемого домена (F-2); ссылки на другие домены перед ним пропускаются; срезать хвостовую пунктуацию. Если первый URL поддерживаемого домена — не поддерживаемое видео, возвращается его ошибка, следующие ссылки не рассматриваются.
2. Хост в нижнем регистре, без `www.` и `m.`; убрать query и fragment, кроме обязательных параметров (их нет). Ссылки с явным портом или userinfo (`user@host`) — `unsupported_url`; длина — до 2048 символов.
3. Распознать платформу и тип:

| Шаблон | Результат |
| --- | --- |
| `tiktok.com/@<user>/video/<id>` | `tiktok:<id>` сразу, без сети |
| `tiktok.com/v/<id>`, `m.tiktok.com/v/<id>` (в т. ч. `<id>.html`) | `tiktok:<id>` |
| `vm.tiktok.com/<code>`, `vt.tiktok.com/<code>`, `tiktok.com/t/<code>` | Короткая — `video_id` узнаёт воркер при резолве |
| `tiktok.com/@<user>/photo/...`, `tiktok.com/@<user>/live` | `unsupported_content` |
| `instagram.com/reel/<code>`, `/reels/<code>`, `/p/<code>` | `instagram:<code>` сразу |
| `instagram.com/stories/...`, `/tv/...` | `unsupported_content` |
| Иное | `unsupported_url` |

### Две формы ссылки

Результат нормализации содержит две строки:

| Поле | Что это | Где используется |
| --- | --- | --- |
| `Key` | Нормализованная форма по правилам выше, без завершающего `/` | Дедупликация, `video_aliases.url_normalized` |
| `URL` | Каноничный адрес, который распознаёт экстрактор yt-dlp | `ResolveTask.url`, `DownloadTask.url`, `SendVideoTask.url` |

Они различаются, потому что экстракторы yt-dlp требуют конкретный хост: TikTok — только `www.tiktok.com`, а форму `/v/<id>` не распознают вовсе.

| Вход | `Key` | `URL` |
| --- | --- | --- |
| `tiktok.com/@user/video/123` | `https://tiktok.com/@user/video/123` | `https://www.tiktok.com/@user/video/123` |
| `m.tiktok.com/v/123.html` | `https://tiktok.com/v/123` | `https://www.tiktok.com/@/video/123` |
| `tiktok.com/t/<code>` | `https://tiktok.com/t/<code>` | `https://www.tiktok.com/t/<code>/` |
| `vm.tiktok.com/<code>` | `https://vm.tiktok.com/<code>` | `https://vm.tiktok.com/<code>/` |
| `instagram.com/reel/<code>` | `https://instagram.com/reel/<code>` | `https://www.instagram.com/reel/<code>/` |

`URL` нормализуется сам в себя (тот же `Key` и `video_id`). Форму `/@/video/<id>` проверить на живой ссылке (`make smoke-live`) при первом прогоне воркера.

Тестовые векторы (вход → ожидаемый результат) лежат в `internal/shared/urls/testdata/vectors.json`; Android-клиент использует тот же файл в своих unit-тестах для клиентской проверки домена.

## Запуск yt-dlp

Внешний процесс, `exec.CommandContext`, отдельная временная директория на задачу, stdout/stderr ограничены 1 МБ.

Общие флаги: `--no-playlist --no-warnings --no-progress --socket-timeout 20 --retries 2 --no-cache-dir --user-agent <актуальный мобильный UA из конфига>`.

| Операция | Флаги | Таймаут |
| --- | --- | --- |
| Резолв | `-J` (метаданные, без скачивания) | 45 с |
| Скачивание для телефона | `-f <phone_selector> --merge-output-format mp4 -o <tmp>/%(id)s.%(ext)s` | 8 мин |
| Скачивание для Telegram | `-f <tg_selector> --merge-output-format mp4` | 8 мин |

### Выбор формата

Требования, а не готовые строки: точные селекторы подобрать и закрепить тестами на реальных ссылках, потому что набор форматов у платформ меняется.

- **Телефон:** лучшее качество, предпочтительно H.264 + AAC в mp4 (максимальная совместимость с галереями), разрешение до 1080p. Жёсткий лимит размера — `max_download_bytes` (500 МБ).
  Отправная точка: `-S "res:1080,vcodec:h264,acodec:aac,ext:mp4" -f "bv*+ba/b"`.
- **Telegram:** лучший формат, итоговый файл которого ≤ `max_send_bytes` (50 МиБ). Сначала по `filesize`/`filesize_approx` из `-J`; после скачивания проверить фактический размер; если больше — повторить с понижением разрешения (1080 → 720 → 540 → 480). Если и 480p больше лимита — `too_large`.
- **TikTok без водяного знака:** исключать форматы, помеченные как watermarked (в метаданных yt-dlp это видно по `format_note`/`format_id`). Если вариантов без водяного знака нет — ошибка `upstream_error` с пометкой для админа, а не тихое скачивание с водяным знаком.

## Instagram и cookies

1. Первая попытка — без cookies.
2. Если stderr указывает на требование входа или лимит (`login required`, `rate-limit reached`, `Requested content is not available`) и в конфиге есть `instagram_cookies` — повтор с `--cookies <tmp>/cookies.txt` (файл создаётся на время задачи с правами `0600` и удаляется).
3. Нет cookies или не помогли → `login_required`; ядро шлёт админу алерт «Instagram требует вход: обновите cookies служебного аккаунта (/cookies)».
4. Cookies — от отдельного служебного аккаунта, не личного. После переезда воркера на новый IP Instagram может потребовать подтверждения входа.

## Сопоставление ошибок yt-dlp

| Признак в stderr / exit | Код |
| --- | --- |
| `Private video`, `This video is private`, `Video unavailable`, `has been removed`, HTTP 404 | `private_video` |
| `login required`, `rate-limit reached` (после попытки с cookies) | `login_required` |
| `No video formats found` на фото/карусели, `There is no video in this post` | `unsupported_content` |
| HTTP 429 | `rate_limited` |
| Таймаут, сетевые ошибки, 5xx | `upstream_error` |
| Прочее | `upstream_error` + stderr (обрезанный) в `message` для логов |

## Обложка

Берётся `thumbnail` из метаданных, качается воркером, приводится к JPEG не больше 720 px по длинной стороне и 512 КБ (`golang.org/x/image/draw`), заливается `PUT /internal/v1/thumbs/{video_id}`. Для `sendVideo` отдельно готовится миниатюра 320 px ≤ 200 КБ.

## Временный кэш воркера

`/tmp/quoker/files`, LRU до 1 ГБ: после `download` файл остаётся, чтобы следующая `send_video` того же видео не качала его заново. Отдельно хранится версия «для Telegram», если пришлось понижать качество. При рестарте контейнера кэш теряется — это нормально.

## Обновление yt-dlp

Версия зашита в образ воркера. Workflow `ytdlp-refresh` раз в неделю пересобирает образ с последним релизом yt-dlp и выкатывает его. Воркер сообщает версию в `Hello`, ядро показывает её в статусе и в ежедневной сводке. Если доля ошибок по платформе за час выше 30% — алерт админу с подсказкой обновить yt-dlp.
