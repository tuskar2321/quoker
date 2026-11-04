package app.quoker.core.data.repository

import app.quoker.core.data.model.Me
import app.quoker.core.data.model.TelegramLink
import app.quoker.core.data.model.TelegramLinkStatus
import app.quoker.core.data.model.toDomain
import app.quoker.core.data.session.SessionStore
import app.quoker.core.network.QuokerApi
import app.quoker.core.network.QuokerException
import app.quoker.core.network.model.RegisterRequestDto
import app.quoker.core.network.quokerCall
import javax.inject.Inject
import kotlin.time.Duration
import kotlin.time.Duration.Companion.seconds
import kotlinx.coroutines.flow.Flow

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

internal class AuthRepositoryImpl @Inject constructor(
    private val api: QuokerApi,
    private val session: SessionStore,
) : AuthRepository {
    override val signedIn: Flow<Boolean> = session.signedIn

    override suspend fun register(inviteCode: String, deviceName: String): Me {
        val res = quokerCall {
            api.register(RegisterRequestDto(inviteCode.trim().uppercase(), session.deviceId(), deviceName.trim()))
        }
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
