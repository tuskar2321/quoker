package app.quoker.feature.history

import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.FileState
import app.quoker.core.data.model.FileStatus
import app.quoker.core.data.model.HistoryAction
import app.quoker.core.data.model.HistoryActionType
import app.quoker.core.data.model.HistoryItem
import app.quoker.core.data.model.HistoryPage
import app.quoker.core.data.model.Platform
import app.quoker.core.data.model.Video
import app.quoker.core.data.repository.HistoryRepository
import app.quoker.core.network.QuokerException
import java.time.Instant
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import org.junit.rules.TestWatcher
import org.junit.runner.Description

fun quokerException(code: String) = QuokerException(QuokerError(code, "server text", retryable = true))

fun action(type: HistoryActionType, at: Instant?, target: String? = null, sendId: Long? = null) =
    HistoryAction(type, target, sendId, at)

fun item(id: Long, vararg actions: HistoryAction, platform: Platform = Platform.TIKTOK) = HistoryItem(
    id = id,
    video = Video(
        id = "tiktok:$id",
        platform = platform,
        sourceUrl = "",
        canonicalUrl = "",
        author = "author$id",
        caption = null,
        durationSeconds = 27,
        sizeBytes = null,
        width = null,
        height = null,
        noWatermark = null,
        thumbnailUrl = null,
        file = FileState(FileStatus.READY, 100),
    ),
    actions = actions.toList(),
)

class FakeHistoryRepository : HistoryRepository {
    val cache = MutableStateFlow(HistoryPage(emptyList(), null))
    var firstPage = HistoryPage(emptyList(), null)
    var refreshError: QuokerException? = null
    /** Pages for cursors; a missing cursor fails with [pageError]. */
    val pages = mutableMapOf<String, HistoryPage>()
    var pageError: QuokerException? = null
    /** When set, page() waits for it before answering. */
    var pageGate: CompletableDeferred<Unit>? = null
    var refreshCalls = 0
    val pageCalls = mutableListOf<String>()

    override val cachedFirstPage: Flow<HistoryPage> = cache

    override suspend fun refresh(limit: Int): HistoryPage {
        refreshCalls++
        refreshError?.let { throw it }
        cache.value = firstPage
        return firstPage
    }

    override suspend fun page(cursor: String, limit: Int): HistoryPage {
        pageCalls += cursor
        pageGate?.await()
        pageError?.let { throw it }
        return pages.getValue(cursor)
    }
}

class MainDispatcherRule(val dispatcher: TestDispatcher = StandardTestDispatcher()) : TestWatcher() {
    override fun starting(description: Description) = Dispatchers.setMain(dispatcher)

    override fun finished(description: Description) = Dispatchers.resetMain()
}
