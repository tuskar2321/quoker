package app.quoker.feature.onboarding

import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.Me
import app.quoker.core.data.model.Role
import app.quoker.core.data.model.TelegramAccount
import app.quoker.core.data.model.TelegramLink
import app.quoker.core.data.model.TelegramLinkStatus
import app.quoker.core.data.repository.AuthRepository
import app.quoker.core.network.QuokerException
import kotlin.time.Duration
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.transformWhile
import kotlinx.coroutines.test.TestDispatcher
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import org.junit.rules.TestWatcher
import org.junit.runner.Description

@OptIn(ExperimentalCoroutinesApi::class)
class MainDispatcherRule(val dispatcher: TestDispatcher = UnconfinedTestDispatcher()) : TestWatcher() {
    override fun starting(description: Description) = Dispatchers.setMain(dispatcher)
    override fun finished(description: Description) = Dispatchers.resetMain()
}

fun testMe(linked: Boolean = false, role: Role = Role.USER) =
    Me(id = 1, role = role, telegram = TelegramAccount(linked, null, null))

fun quokerException(code: String, retryable: Boolean = false) =
    QuokerException(QuokerError(code, "server text", retryable))

/** Records calls; [pollStatuses] drives pollTelegramStatus (its subscriptionCount shows polling). */
class FakeAuthRepository : AuthRepository {
    val registered = mutableListOf<Pair<String, String>>()
    var registerResult: () -> Me = { testMe() }
    var linkResult: () -> TelegramLink = { TelegramLink("https://t.me/bot?start=link_${++linksCreated}", null) }
    var linksCreated = 0
    val pollStatuses = MutableSharedFlow<TelegramLinkStatus>()
    /** Throws from the poll flow when set. */
    var pollFailure: QuokerException? = null

    override val signedIn: Flow<Boolean> = MutableStateFlow(false)

    override suspend fun register(inviteCode: String, deviceName: String): Me {
        registered += inviteCode to deviceName
        return registerResult()
    }

    override suspend fun logout() = Unit
    override suspend fun me(): Me = testMe()
    override suspend fun createTelegramLink(): TelegramLink = linkResult()
    override suspend fun telegramStatus(): TelegramLinkStatus = error("not used")

    override fun pollTelegramStatus(interval: Duration): Flow<TelegramLinkStatus> {
        pollFailure?.let { failure -> return kotlinx.coroutines.flow.flow { throw failure } }
        return pollStatuses.transformWhile { emit(it); !it.state.isTerminal }
    }

    override suspend fun unlinkTelegram() = Unit
}
