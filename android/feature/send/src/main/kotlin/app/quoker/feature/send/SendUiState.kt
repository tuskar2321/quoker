package app.quoker.feature.send

import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.Target
import app.quoker.core.data.model.TargetType

/** A row of the "Куда отправить" list. */
data class TargetItem(
    val target: Target,
    /** The most recently used active target: marked «последняя». */
    val isLastUsed: Boolean,
) {
    val id: Long get() = target.id
    val enabled: Boolean get() = target.isActive
}

data class SendUiState(
    /** The sheet opening this state belongs to; the UI treats a stale session as loading. */
    val sessionId: String? = null,
    /** Active targets first (server order), then inactive ones; hidden ones never. */
    val targets: List<TargetItem> = emptyList(),
    /** True once GET /v1/targets answered at least once in this session. */
    val fetched: Boolean = false,
    val fetchError: QuokerError? = null,
    val refreshing: Boolean = false,
    val selectedId: Long? = null,
    /** Shown while the user has not edited the caption (the link the core adds by default). */
    val defaultCaption: String = "",
    val captionText: String = "",
    val captionTouched: Boolean = false,
    val signAsUser: Boolean = true,
    /** «@username» or the first name; null while unknown. */
    val signName: String? = null,
    val sending: Boolean = false,
    val sendError: QuokerError? = null,
) {
    val selected: TargetItem? get() = targets.firstOrNull { it.id == selectedId && it.enabled }

    val hasActiveTargets: Boolean get() = targets.any { it.enabled }

    /** Nothing cached yet and the first fetch is still running. */
    val loading: Boolean get() = targets.isEmpty() && !fetched && fetchError == null

    /** The first fetch failed and there is nothing cached to show. */
    val loadFailed: Boolean get() = targets.isEmpty() && !fetched && fetchError != null

    /** «Пока некуда отправлять». */
    val empty: Boolean get() = fetched && !hasActiveTargets

    val captionFieldValue: String get() = if (captionTouched) captionText else defaultCaption

    /** null: the core adds the original link; "": no caption. */
    val captionToSend: String? get() = if (captionTouched) captionText else null

    val captionLeft: Int get() = CAPTION_MAX_LENGTH - captionFieldValue.length

    val showSignToggle: Boolean get() = selected?.target?.type == TargetType.GROUP

    val canSend: Boolean get() = selected != null && !sending
}

sealed interface SendEvent {
    data object Sent : SendEvent

    data object LinkTelegram : SendEvent
}
