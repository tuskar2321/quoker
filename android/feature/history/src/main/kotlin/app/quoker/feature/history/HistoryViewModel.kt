package app.quoker.feature.history

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.quoker.core.common.Clock
import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.HistoryItem
import app.quoker.core.data.model.HistoryPage
import app.quoker.core.data.repository.HistoryRepository
import app.quoker.core.network.ErrorCodes
import app.quoker.core.network.QuokerException
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.ZoneId
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class HistoryUiState(
    val sections: List<HistorySection> = emptyList(),
    /** The core answered at least once, or the cache had items: an empty list is really empty. */
    val loaded: Boolean = false,
    val refreshing: Boolean = false,
    /** Error of the last first-page load. */
    val error: QuokerError? = null,
    val loadingMore: Boolean = false,
    val loadMoreError: QuokerError? = null,
    val endReached: Boolean = true,
) {
    val isEmpty: Boolean get() = sections.isEmpty()
    val showSkeleton: Boolean get() = isEmpty && !loaded && error == null
    val showEmpty: Boolean get() = isEmpty && loaded && error == null
    val showError: Boolean get() = isEmpty && error != null
    /** Content is shown from the cache, but the core is unreachable. */
    val offline: Boolean get() = !isEmpty && error?.code == ErrorCodes.NETWORK
    /** Any other refresh error over cached content. */
    val bannerError: QuokerError? get() = error?.takeIf { !isEmpty && it.code != ErrorCodes.NETWORK }
}

@HiltViewModel
class HistoryViewModel @Inject constructor(
    private val history: HistoryRepository,
    private val clock: Clock,
) : ViewModel() {
    private val zone: ZoneId = ZoneId.systemDefault()

    private val _state = MutableStateFlow(HistoryUiState())
    val state: StateFlow<HistoryUiState> = _state.asStateFlow()

    private var firstPage: HistoryPage? = null
    private var more: List<HistoryItem> = emptyList()
    private var cursor: String? = null
    /** Bumped whenever the first page changes, so a stale "load more" result is dropped. */
    private var generation = 0

    init {
        viewModelScope.launch {
            history.cachedFirstPage.collect { page ->
                if (page != firstPage) applyFirstPage(page, fromServer = false)
            }
        }
        refresh()
    }

    fun refresh() {
        if (_state.value.refreshing) return
        _state.update { it.copy(refreshing = true) }
        viewModelScope.launch {
            try {
                val page = history.refresh(HistoryRepository.DEFAULT_LIMIT)
                if (page != firstPage) applyFirstPage(page, fromServer = true)
                _state.update { it.copy(refreshing = false, loaded = true, error = null) }
            } catch (e: QuokerException) {
                _state.update { it.copy(refreshing = false, error = e.error) }
            }
        }
    }

    /** Called when the list nears its end. */
    fun loadMore() {
        val s = _state.value
        val next = cursor ?: return
        if (s.loadingMore || s.loadMoreError != null || s.refreshing) return
        val gen = generation
        _state.update { it.copy(loadingMore = true) }
        viewModelScope.launch {
            try {
                val page = history.page(next, HistoryRepository.DEFAULT_LIMIT)
                if (gen != generation) return@launch
                more = more + page.items
                cursor = page.nextCursor
                publish()
            } catch (e: QuokerException) {
                if (gen == generation) _state.update { it.copy(loadMoreError = e.error) }
            } finally {
                if (gen == generation) _state.update { it.copy(loadingMore = false) }
            }
        }
    }

    fun retryLoadMore() {
        _state.update { it.copy(loadMoreError = null) }
        loadMore()
    }

    private fun applyFirstPage(page: HistoryPage, fromServer: Boolean) {
        generation++
        firstPage = page
        more = emptyList()
        cursor = page.nextCursor
        _state.update {
            it.copy(
                loaded = it.loaded || fromServer || page.items.isNotEmpty(),
                loadingMore = false,
                loadMoreError = null,
            )
        }
        publish()
    }

    private fun publish() {
        val items = (firstPage?.items.orEmpty() + more)
        _state.update {
            it.copy(sections = buildSections(items, clock.now(), zone), endReached = cursor == null)
        }
    }
}
