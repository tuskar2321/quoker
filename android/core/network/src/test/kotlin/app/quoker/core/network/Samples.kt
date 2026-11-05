package app.quoker.core.network

/**
 * Response bodies in the exact shape the Go handlers produce (internal/core/httpapi): every
 * field present, zero values rendered as null, RFC 3339 UTC without fractions.
 */
object Samples {
    const val VIDEO = """{"id":"tiktok:7301234567890123456","platform":"tiktok","source_url":"https://vm.tiktok.com/ZMabc/","canonical_url":"https://www.tiktok.com/@author/video/7301234567890123456","author":"author","caption":"Описание автора","duration_s":27,"size_bytes":14680064,"width":1080,"height":1920,"no_watermark":true,"thumbnail_url":"/v1/videos/tiktok:7301234567890123456/thumbnail","file":{"status":"ready","size_bytes":14680064}}"""

    // Instagram video the core has partly forgotten: no thumbnail, no metadata, no no_watermark.
    const val VIDEO_BARE = """{"id":"instagram:C1a2b3","platform":"instagram","source_url":"https://www.instagram.com/reel/C1a2b3/","canonical_url":"https://www.instagram.com/reel/C1a2b3/","author":null,"caption":null,"duration_s":null,"size_bytes":null,"width":null,"height":null,"no_watermark":null,"thumbnail_url":null,"file":{"status":"none","size_bytes":null}}"""

    const val ME = """{"id":7,"role":"admin","telegram":{"linked":true,"username":"max","first_name":"Max"}}"""
    const val ME_UNLINKED = """{"id":8,"role":"user","telegram":{"linked":false,"username":null,"first_name":null}}"""
    const val REGISTER = """{"session_token":"s3cr3t-token","user":$ME_UNLINKED}"""
    const val TELEGRAM_LINK = """{"deep_link":"https://t.me/quoker_bot?start=link_abc","expires_at":"2026-11-03T12:10:00Z"}"""
    const val TELEGRAM_STATUS_PENDING = """{"status":"pending"}"""
    const val TELEGRAM_STATUS_LINKED = """{"status":"linked","username":"max","first_name":"Max"}"""

    const val RESOLVE_200 = """{"video":$VIDEO}"""
    const val TASK_ID = """{"task_id":"t_01JABC"}"""
    const val TASK_RUNNING = """{"id":"t_01JABC","kind":"resolve","status":"running","progress":40,"video":null,"error":null}"""
    const val TASK_QUEUED = """{"id":"t_01JABC","kind":"resolve","status":"queued","progress":null,"video":null,"error":null}"""
    const val TASK_DONE = """{"id":"t_01JABC","kind":"resolve","status":"done","progress":100,"video":$VIDEO,"error":null}"""
    const val TASK_FAILED = """{"id":"t_01JABC","kind":"download","status":"failed","progress":null,"video":null,"error":{"code":"private_video","message":"Видео приватное или удалено","retryable":false}}"""
    const val PREPARE_200 = """{"file":{"status":"ready","size_bytes":14680064}}"""

    const val TARGET = """{"id":42,"title":"Друзья","type":"group","status":"active","inactive_reason":null,"hidden":false,"last_used_at":"2026-11-02T12:40:00Z"}"""
    const val TARGET_INACTIVE = """{"id":43,"title":"Канал","type":"channel","status":"inactive","inactive_reason":"bot_not_admin","hidden":true,"last_used_at":null}"""
    const val TARGETS = """{"items":[$TARGET,$TARGET_INACTIVE]}"""
    const val TARGET_PATCHED = """{"target":$TARGET_INACTIVE}"""

    const val SEND_QUEUED = """{"send":{"id":1001,"video_id":"tiktok:7301234567890123456","target_id":42,"status":"queued","error":null,"created_at":"2026-11-03T12:00:00Z","updated_at":"2026-11-03T12:00:00Z"}}"""
    const val SEND_SENT = """{"send":{"id":1001,"video_id":"tiktok:7301234567890123456","target_id":42,"status":"sent","error":null,"created_at":"2026-11-03T12:00:00Z","updated_at":"2026-11-03T12:00:05Z"}}"""
    const val SEND_FAILED = """{"send":{"id":1001,"video_id":"tiktok:7301234567890123456","target_id":42,"status":"failed","error":{"code":"target_inactive","message":"Чат недоступен для отправки","retryable":false},"created_at":"2026-11-03T12:00:00Z","updated_at":"2026-11-03T12:00:05Z"}}"""

    const val HISTORY = """{"items":[{"id":5,"video":$VIDEO,"actions":[{"type":"sent","target_title":"Друзья","send_id":1001,"at":"2026-11-03T12:00:05Z"},{"type":"downloaded","target_title":null,"send_id":null,"at":"2026-11-03T11:59:00Z"},{"type":"resolved","target_title":null,"send_id":null,"at":"2026-11-03T11:58:00Z"}]},{"id":4,"video":$VIDEO_BARE,"actions":[{"type":"send_failed","target_title":"Канал","send_id":1000,"at":"2026-11-02T10:00:00Z"}]}],"next_cursor":"c_4"}"""
    const val HISTORY_LAST = """{"items":[],"next_cursor":null}"""

    const val APP_VERSION = """{"version_code":12,"version_name":"1.2.0","apk_url":"https://api.example.ru/download/quoker-1.2.0.apk","sha256":"ab12","min_supported_version_code":10,"changelog":"Новое"}"""

    const val WORKER = """{"id":"ru-1","fingerprint":"deadbeef","status":"allowed","connected":true,"version":"1.0.0","ytdlp_version":"2026.10.01","cert_not_after":"2027-11-12T00:00:00Z","last_seen_at":"2026-11-03T11:59:59Z","is_poller":true}"""
    const val WORKER_OFFLINE = """{"id":"nl-1","fingerprint":"cafe","status":"revoked","connected":false,"version":null,"ytdlp_version":null,"last_seen_at":null,"is_poller":false}"""
    const val ADMIN_STATUS = """{"workers":[$WORKER,$WORKER_OFFLINE],"queue":{"queued":3,"running":1},"errors_24h":{"instagram":2,"telegram":0,"tiktok":1},"disk":{"used_bytes":1000,"total_bytes":4000,"cache_bytes":500},"versions":{"app_latest":"","core":"v1.0.0"}}"""
    const val WORKERS = """{"items":[$WORKER]}"""
    const val INVITES = """{"items":[{"id":3,"role":"user","code_prefix":"K7Q2","created_at":"2026-11-01T10:00:00Z","expires_at":"2026-11-08T10:00:00Z","used_by":null,"used_at":null},{"id":2,"role":"admin","code_prefix":"AB12","created_at":"2026-10-01T10:00:00Z","expires_at":"2026-10-08T10:00:00Z","used_by":8,"used_at":"2026-10-02T10:00:00Z"}]}"""
    const val INVITE_CREATED = """{"code":"K7Q2-ZX9P","expires_at":"2026-11-10T12:00:00Z"}"""
    const val USERS = """{"items":[{"id":7,"role":"admin","status":"active","telegram_username":"max","telegram_first_name":"Max","targets_count":3,"created_at":"2026-10-01T10:00:00Z","last_seen_at":"2026-11-03T11:00:00Z"},{"id":8,"role":"user","status":"blocked","telegram_username":null,"telegram_first_name":null,"targets_count":0,"created_at":"2026-10-02T10:00:00Z","last_seen_at":null}]}"""

    fun error(code: String, message: String = "msg", retryable: Boolean = false) =
        """{"error":{"code":"$code","message":"$message","retryable":$retryable}}"""
}
