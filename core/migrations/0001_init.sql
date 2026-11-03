-- Quoker core schema, SQLite. See docs/05-data-model.md.
-- Times are stored as INTEGER unix milliseconds (UTC).

PRAGMA foreign_keys = ON;

CREATE TABLE users (
    id              INTEGER PRIMARY KEY,
    role            TEXT    NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    status          TEXT    NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'blocked')),
    invite_id       INTEGER REFERENCES invites (id),
    tg_user_id      INTEGER UNIQUE,
    tg_username     TEXT,
    tg_first_name   TEXT,
    tg_linked_at    INTEGER,
    created_at      INTEGER NOT NULL,
    last_seen_at    INTEGER
);

CREATE TABLE invites (
    id              INTEGER PRIMARY KEY,
    code_hash       BLOB    NOT NULL UNIQUE,          -- SHA-256 of the normalized code
    role            TEXT    NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    created_by      INTEGER REFERENCES users (id),    -- NULL for the bootstrap invite
    created_at      INTEGER NOT NULL,
    expires_at      INTEGER NOT NULL,
    used_by         INTEGER REFERENCES users (id),
    used_at         INTEGER,
    revoked_at      INTEGER
);

CREATE TABLE sessions (
    id              INTEGER PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users (id),
    token_hash      BLOB    NOT NULL UNIQUE,          -- SHA-256 of the session token
    device_id       TEXT    NOT NULL,
    device_name     TEXT    NOT NULL,
    app_version     INTEGER,
    created_at      INTEGER NOT NULL,
    last_seen_at    INTEGER NOT NULL,
    revoked_at      INTEGER
);
CREATE INDEX sessions_user ON sessions (user_id);

CREATE TABLE link_tokens (
    token_hash      BLOB    PRIMARY KEY,              -- SHA-256 of the deep-link token
    user_id         INTEGER NOT NULL REFERENCES users (id),
    created_at      INTEGER NOT NULL,
    expires_at      INTEGER NOT NULL,
    used_at         INTEGER
);

CREATE TABLE targets (
    id              INTEGER PRIMARY KEY,
    owner_user_id   INTEGER NOT NULL REFERENCES users (id),
    chat_id         INTEGER NOT NULL,                 -- Telegram chat id
    type            TEXT    NOT NULL CHECK (type IN ('group', 'channel', 'private')),
    title           TEXT    NOT NULL,
    status          TEXT    NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    inactive_reason TEXT CHECK (inactive_reason IN ('bot_removed', 'bot_not_admin', 'user_left', 'user_not_admin')),
    hidden          INTEGER NOT NULL DEFAULT 0,
    rights_checked_at INTEGER,
    last_used_at    INTEGER,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    UNIQUE (owner_user_id, chat_id)
);
CREATE INDEX targets_chat ON targets (chat_id);

CREATE TABLE videos (
    id              TEXT    PRIMARY KEY,              -- "<platform>:<native_id>"
    platform        TEXT    NOT NULL CHECK (platform IN ('tiktok', 'instagram')),
    native_id       TEXT    NOT NULL,
    canonical_url   TEXT    NOT NULL,
    author          TEXT,
    caption         TEXT,
    duration_s      INTEGER,
    size_bytes      INTEGER,
    width           INTEGER,
    height          INTEGER,
    no_watermark    INTEGER,
    has_thumbnail   INTEGER NOT NULL DEFAULT 0,
    file_status     TEXT    NOT NULL DEFAULT 'none' CHECK (file_status IN ('none', 'preparing', 'ready', 'failed')),
    file_size       INTEGER,
    file_sha256     TEXT,
    resolved_at     INTEGER NOT NULL,
    expires_at      INTEGER NOT NULL,                 -- metadata + file TTL, 24h after last access
    last_access_at  INTEGER NOT NULL
);
CREATE INDEX videos_expires ON videos (expires_at);

-- Raw URLs users submitted, so the same short link is not resolved twice.
CREATE TABLE video_aliases (
    url_normalized  TEXT    PRIMARY KEY,
    video_id        TEXT    NOT NULL REFERENCES videos (id) ON DELETE CASCADE,
    created_at      INTEGER NOT NULL
);

CREATE TABLE tasks (
    id              TEXT    PRIMARY KEY,              -- ULID
    kind            TEXT    NOT NULL CHECK (kind IN ('resolve', 'download', 'send_video', 'bot_call')),
    payload         BLOB    NOT NULL,                 -- protobuf-encoded task kind
    status          TEXT    NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'leased', 'done', 'failed', 'cancelled')),
    priority        INTEGER NOT NULL DEFAULT 0,       -- bot_call > resolve > send_video > download
    attempts        INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL,
    worker_id       TEXT,
    lease_until     INTEGER,
    progress        INTEGER,
    result          BLOB,                             -- protobuf-encoded result
    error_code      TEXT,
    error_message   TEXT,
    dedup_key       TEXT,                             -- e.g. "download:tiktok:123"; one active task per key
    user_id         INTEGER REFERENCES users (id),
    not_before      INTEGER NOT NULL,                 -- backoff for retries
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);
CREATE INDEX tasks_pick ON tasks (status, priority DESC, not_before, created_at);
CREATE UNIQUE INDEX tasks_dedup_active ON tasks (dedup_key) WHERE status IN ('queued', 'leased') AND dedup_key IS NOT NULL;

CREATE TABLE sends (
    id              INTEGER PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users (id),
    video_id        TEXT    NOT NULL,
    target_id       INTEGER NOT NULL REFERENCES targets (id),
    idempotency_key TEXT    NOT NULL,
    caption_html    TEXT,
    status          TEXT    NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'sending', 'sent', 'failed')),
    task_id         TEXT    REFERENCES tasks (id),
    tg_message_id   INTEGER,
    error_code      TEXT,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    UNIQUE (user_id, idempotency_key)
);

CREATE TABLE history (
    id              INTEGER PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users (id),
    video_id        TEXT    NOT NULL,
    action          TEXT    NOT NULL CHECK (action IN ('resolved', 'downloaded', 'sent', 'send_failed')),
    target_title    TEXT,
    send_id         INTEGER REFERENCES sends (id),
    created_at      INTEGER NOT NULL
);
CREATE INDEX history_user ON history (user_id, created_at DESC);

-- Video metadata needed by history must outlive the 24h cache.
CREATE TABLE history_videos (
    video_id        TEXT    PRIMARY KEY,
    platform        TEXT    NOT NULL,
    canonical_url   TEXT    NOT NULL,
    author          TEXT,
    duration_s      INTEGER,
    size_bytes      INTEGER
);

CREATE TABLE workers (
    id              TEXT    PRIMARY KEY,              -- certificate CN
    fingerprint     TEXT    NOT NULL UNIQUE,          -- SHA-256 hex of the certificate
    status          TEXT    NOT NULL DEFAULT 'allowed' CHECK (status IN ('allowed', 'revoked')),
    cert_not_after  INTEGER,
    version         TEXT,
    ytdlp_version   TEXT,
    created_at      INTEGER NOT NULL,
    last_seen_at    INTEGER
);

CREATE TABLE bot_updates (
    update_id       INTEGER PRIMARY KEY,              -- idempotency for forwarded updates
    processed_at    INTEGER NOT NULL
);

-- Secrets and runtime settings: bot_token, instagram_cookies, bot_username,
-- min_supported_version_code, admin_chat_id, poller_worker_id.
CREATE TABLE settings (
    key             TEXT    PRIMARY KEY,
    value           TEXT    NOT NULL,
    updated_at      INTEGER NOT NULL
);

CREATE TABLE app_releases (
    version_code    INTEGER PRIMARY KEY,
    version_name    TEXT    NOT NULL,
    apk_path        TEXT    NOT NULL,
    sha256          TEXT    NOT NULL,
    changelog       TEXT    NOT NULL DEFAULT '',
    published_at    INTEGER NOT NULL
);

CREATE TABLE rate_limits (
    user_id         INTEGER NOT NULL,
    bucket          TEXT    NOT NULL,                 -- resolve | send
    window_start    INTEGER NOT NULL,                 -- start of the hour, unix ms
    count           INTEGER NOT NULL,
    PRIMARY KEY (user_id, bucket, window_start)
);
