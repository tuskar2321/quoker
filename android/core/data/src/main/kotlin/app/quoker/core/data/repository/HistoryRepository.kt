package app.quoker.core.data.repository

import app.quoker.core.common.Clock
import app.quoker.core.common.di.IoDispatcher
import app.quoker.core.data.db.HistoryDao
import app.quoker.core.data.db.HistoryItemEntity
import app.quoker.core.data.db.HistoryMetaEntity
import app.quoker.core.data.model.HistoryPage
import app.quoker.core.data.model.toDomain
import app.quoker.core.network.ApiUrls
import app.quoker.core.network.QuokerApi
import app.quoker.core.network.model.HistoryActionDto
import app.quoker.core.network.model.HistoryItemDto
import app.quoker.core.network.model.VideoDto
import app.quoker.core.network.quokerCall
import javax.inject.Inject
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flowOn
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json

interface HistoryRepository {
    /**
     * The cached first page for instant display (empty page before the first [refresh]). Updates
     * after every [refresh]. Its nextCursor continues with [page].
     */
    val cachedFirstPage: Flow<HistoryPage>

    /** Loads the first page from the core and replaces the cache. */
    suspend fun refresh(limit: Int = DEFAULT_LIMIT): HistoryPage

    /** Loads a following page (not cached). */
    suspend fun page(cursor: String, limit: Int = DEFAULT_LIMIT): HistoryPage

    companion object {
        const val DEFAULT_LIMIT = 20
        const val MAX_LIMIT = 50
    }
}

internal class HistoryRepositoryImpl @Inject constructor(
    private val api: QuokerApi,
    private val dao: HistoryDao,
    private val urls: ApiUrls,
    private val json: Json,
    private val clock: Clock,
    @IoDispatcher private val io: CoroutineDispatcher,
) : HistoryRepository {
    private val actionsSerializer = ListSerializer(HistoryActionDto.serializer())

    override val cachedFirstPage: Flow<HistoryPage> =
        combine(dao.observeItems(), dao.observeMeta()) { rows, meta ->
            HistoryPage(
                items = rows.map { it.toDto().toDomain(urls) },
                nextCursor = meta?.nextCursor,
            )
        }.flowOn(io)

    override suspend fun refresh(limit: Int): HistoryPage {
        val page = quokerCall { api.history(cursor = null, limit = limit) }
        dao.replaceFirstPage(
            items = page.items.mapIndexed { i, item -> item.toEntity(i) },
            meta = HistoryMetaEntity(nextCursor = page.nextCursor, fetchedAtMillis = clock.now().toEpochMilli()),
        )
        return page.toDomain(urls)
    }

    override suspend fun page(cursor: String, limit: Int): HistoryPage =
        quokerCall { api.history(cursor = cursor, limit = limit) }.toDomain(urls)

    private fun HistoryItemDto.toEntity(position: Int) = HistoryItemEntity(
        id = id,
        position = position,
        videoJson = json.encodeToString(VideoDto.serializer(), video),
        actionsJson = json.encodeToString(actionsSerializer, actions),
    )

    private fun HistoryItemEntity.toDto() = HistoryItemDto(
        id = id,
        video = json.decodeFromString(VideoDto.serializer(), videoJson),
        actions = json.decodeFromString(actionsSerializer, actionsJson),
    )
}
