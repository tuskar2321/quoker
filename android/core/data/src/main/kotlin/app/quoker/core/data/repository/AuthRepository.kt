package app.quoker.core.data.repository

import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.Me
import app.quoker.core.data.model.TelegramLink
import app.quoker.core.data.model.TelegramLinkStatus
import app.quoker.core.data.model.toDomain
import app.quoker.core.data.session.SessionStore
import app.quoker.core.network.ErrorCodes
import app.quoker.core.network.QuokerApi
import app.quoker.core.network.QuokerException
import app.quoker.core.network.model.RegisterRequestDto
import app.quoker.core.network.quokerCall
import javax.inject.Inject
import kotlin.coroutines.CoroutineContext
import kotlin.time.Duration
import kotlin.time.Duration.Companion.seconds
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull

interface AuthRepository {
    val signedIn: Flow<Boolean>

    /** Redeems an invite (`XXXX-XXXX`) and stores the session. */
    suspend fun register(inviteCode: String, deviceName: String): Me

    /** Revokes the session on the core (best effort) and always signs out locally. */
    suspend fun logout()

    suspend fun me(): Me

    suspend fun createTelegramLink(): TelegramLink

    suspend fun telegramStatus(): TelegramLinkStatus

    /** Polls the binding status until LINKED or EXPIRED. */
    fun pollTelegramStatus(interval: Duration = TELEGRAM_POLL_INTERVAL): Flow<TelegramLinkStatus>

    suspend fun unlinkTelegram()

    companion object {
        val TELEGRAM_POLL_INTERVAL: Duration = 2.seconds
    }
}

internal class AuthRepositoryImpl(
    private val api: QuokerApi,
    private val session: SessionStore,
    private val registerTimeout: Duration,
    private val timeoutContext: CoroutineContext,
) : AuthRepository {
    // The timeout runs on a real clock (Dispatchers.IO), not on a test scheduler's virtual time.
    @Inject
    constructor(api: QuokerApi, session: SessionStore) : this(api, session, REGISTER_TIMEOUT, Dispatchers.IO)

    override val signedIn: Flow<Boolean> = session.signedIn

    override suspend fun register(inviteCode: String, deviceName: String): Me {
        // One limit for the whole sign-in call (docs/08): OkHttp's per-phase timeouts and route
        // retries could otherwise keep «Продолжить» spinning far longer when the core is unreachable.
        val res = withContext(timeoutContext) {
            withTimeoutOrNull(registerTimeout) {
                quokerCall {
                    api.register(RegisterRequestDto(inviteCode.trim().uppercase(), session.deviceId(), deviceName.trim()))
                }
            }
        } ?: throw QuokerException(QuokerError(ErrorCodes.NETWORK, "sign-in timed out", retryable = true))
        session.saveToken(res.sessionToken)
        return res.user.toDomain()
    }

    override suspend fun logout() {
        try {
            quokerCall { api.logout() }
        } catch (_: QuokerException) {
            // Signing out must work offline; the session expires on the core anyway.
        } finally {
            session.clear()
        }
    }

    override suspend fun me(): Me = quokerCall { api.me() }.toDomain()

    override suspend fun createTelegramLink(): TelegramLink = quokerCall { api.createTelegramLink() }.toDomain()

    override suspend fun telegramStatus(): TelegramLinkStatus = quokerCall { api.telegramStatus() }.toDomain()

    override fun pollTelegramStatus(interval: Duration): Flow<TelegramLinkStatus> =
        poll(interval, { api.telegramStatus().toDomain() }) { it.state.isTerminal }

    override suspend fun unlinkTelegram() = quokerCall { api.unlinkTelegram() }
}

/** Sign-in by invite code gives up after this long (docs/08 «Сеть»). */
internal val REGISTER_TIMEOUT: Duration = 10.seconds

