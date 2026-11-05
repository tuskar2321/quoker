package app.quoker.core.data.model

import java.time.Duration
import java.time.Instant

/** docs/08, docs/06: the admin is warned only after no worker has been connected for this long. */
val NO_WORKERS_ALERT_AFTER: Duration = Duration.ofMinutes(5)

/**
 * No allowed worker is connected, and none was seen within [NO_WORKERS_ALERT_AFTER] of [now].
 * Without any allowed worker, or when none was ever seen, there is nothing to wait for: true at once.
 */
fun List<Worker>.noWorkersAlert(now: Instant): Boolean {
    val allowed = filter { it.status == WorkerStatus.ALLOWED }
    if (allowed.any { it.connected }) return false
    val lastSeen = allowed.mapNotNull { it.lastSeenAt }.maxOrNull() ?: return true
    return Duration.between(lastSeen, now) >= NO_WORKERS_ALERT_AFTER
}
