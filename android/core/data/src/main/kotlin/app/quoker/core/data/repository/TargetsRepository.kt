package app.quoker.core.data.repository

import app.quoker.core.data.db.TargetDao
import app.quoker.core.data.db.TargetEntity
import app.quoker.core.data.model.Target
import app.quoker.core.data.model.toDomain
import app.quoker.core.network.QuokerApi
import app.quoker.core.network.model.PatchTargetRequestDto
import app.quoker.core.network.model.TargetDto
import app.quoker.core.network.quokerCall
import javax.inject.Inject
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

interface TargetsRepository {
    /** Cached targets in server order (last used first, then by title); hidden ones only when asked. */
    fun observe(includeHidden: Boolean = false): Flow<List<Target>>

    /** GET /v1/targets (hidden included) and replace the cache. */
    suspend fun fetch(): List<Target>

    suspend fun setHidden(targetId: Long, hidden: Boolean): Target

    /**
     * POST /v1/targets/refresh: queues a rights re-check on the core (results arrive in the
     * background; [fetch] again later). Rate limited to once a minute.
     */
    suspend fun refresh()
}

internal class TargetsRepositoryImpl @Inject constructor(
    private val api: QuokerApi,
    private val dao: TargetDao,
) : TargetsRepository {
    override fun observe(includeHidden: Boolean): Flow<List<Target>> = dao.observeAll().map { rows ->
        rows.asSequence().filter { includeHidden || !it.hidden }.map { it.toDto().toDomain() }.toList()
    }

    override suspend fun fetch(): List<Target> {
        val items = quokerCall { api.targets(includeHidden = true) }.items
        dao.replaceAll(items.mapIndexed { i, t -> t.toEntity(i) })
        return items.map { it.toDomain() }
    }

    override suspend fun setHidden(targetId: Long, hidden: Boolean): Target {
        val t = quokerCall { api.patchTarget(targetId, PatchTargetRequestDto(hidden)) }.target
        dao.upsert(listOf(t.toEntity(dao.positionOf(t.id) ?: Int.MAX_VALUE)))
        return t.toDomain()
    }

    override suspend fun refresh() = quokerCall { api.refreshTargets() }
}

private fun TargetDto.toEntity(position: Int) =
    TargetEntity(id, position, title, type, status, inactiveReason, hidden, lastUsedAt)

private fun TargetEntity.toDto() = TargetDto(id, title, type, status, inactiveReason, hidden, lastUsedAt)
