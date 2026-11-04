package app.quoker.core.data.repository

import app.quoker.core.data.model.Send
import app.quoker.core.data.model.toDomain
import app.quoker.core.network.QuokerApi
import app.quoker.core.network.model.CreateSendRequestDto
import app.quoker.core.network.quokerCall
import java.util.UUID
import javax.inject.Inject
import kotlin.time.Duration
import kotlin.time.Duration.Companion.seconds
import kotlinx.coroutines.flow.Flow

interface SendsRepository {
    /** A fresh key per "Куда отправить" sheet; reuse it for every retry of the same send. */
    fun newIdempotencyKey(): String

    /**
     * POST /v1/sends. [caption]: null lets the core add the original link, "" sends no caption.
     * [signAsUser] matters for groups only.
     */
    suspend fun create(
        videoId: String,
        targetId: Long,
        caption: String?,
        signAsUser: Boolean,
        idempotencyKey: String,
    ): Send

    suspend fun get(sendId: Long): Send

    /** Emits the send every second until SENT or FAILED; stop collecting when the sheet closes. */
    fun poll(sendId: Long, interval: Duration = SEND_POLL_INTERVAL): Flow<Send>

    companion object {
        val SEND_POLL_INTERVAL: Duration = 1.seconds
    }
}

internal class SendsRepositoryImpl @Inject constructor(
    private val api: QuokerApi,
) : SendsRepository {
    override fun newIdempotencyKey(): String = UUID.randomUUID().toString()

    override suspend fun create(
        videoId: String,
        targetId: Long,
        caption: String?,
        signAsUser: Boolean,
        idempotencyKey: String,
    ): Send = quokerCall {
        api.createSend(idempotencyKey, CreateSendRequestDto(videoId, targetId, caption, signAsUser))
    }.send.toDomain()

    override suspend fun get(sendId: Long): Send = quokerCall { api.send(sendId) }.send.toDomain()

    override fun poll(sendId: Long, interval: Duration): Flow<Send> =
        poll(interval, { api.send(sendId).send.toDomain() }) { it.status.isTerminal }
}
