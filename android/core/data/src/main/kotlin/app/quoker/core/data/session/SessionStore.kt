package app.quoker.core.data.session

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import app.quoker.core.network.DeviceIdProvider
import app.quoker.core.network.SessionEvent
import app.quoker.core.network.SessionEvents
import app.quoker.core.network.SessionTokenProvider
import java.util.UUID
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterIsInstance
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking

/** Session token and device id. Also the token/device source of the HTTP layer. */
interface SessionStore : SessionTokenProvider, DeviceIdProvider {
    /** True while a session token is stored. */
    val signedIn: Flow<Boolean>

    suspend fun saveToken(token: String)

    /** Drops the token and cached user data; the device id stays. */
    suspend fun clear()
}

/** Wipes per-user caches (Room) on sign-out. */
fun interface CachedDataCleaner {
    suspend fun clear()
}

/**
 * DataStore-backed [SessionStore]; the token is stored encrypted by [TokenCipher]. Clears itself
 * on [SessionEvent.SignedOut]. Token and device id are cached in memory after the first read, so
 * the blocking provider calls from OkHttp threads hit DataStore at most once.
 */
class DataStoreSessionStore(
    private val dataStore: DataStore<Preferences>,
    private val cipher: TokenCipher,
    private val cleaner: CachedDataCleaner,
    events: SessionEvents,
    scope: CoroutineScope,
) : SessionStore {
    private sealed interface Cached {
        data object Unknown : Cached

        data class Known(val token: String?) : Cached
    }

    @Volatile private var token: Cached = Cached.Unknown

    @Volatile private var deviceId: String? = null

    init {
        // Undispatched so the subscription exists before the first request can fail.
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            events.events.filterIsInstance<SessionEvent.SignedOut>().collect { clear() }
        }
    }

    override val signedIn: Flow<Boolean> = dataStore.data
        .map { it[KEY_TOKEN] != null }
        .distinctUntilChanged()

    override fun sessionToken(): String? = when (val cached = token) {
        is Cached.Known -> cached.token
        Cached.Unknown -> runBlocking { loadToken() }
    }

    private suspend fun loadToken(): String? {
        val stored = dataStore.data.first()[KEY_TOKEN]
        val plain = stored?.let(cipher::decrypt)
        if (stored != null && plain == null) {
            // Undecryptable (Keystore key lost): treat as signed out.
            dataStore.edit { it.remove(KEY_TOKEN) }
        }
        token = Cached.Known(plain)
        return plain
    }

    override suspend fun saveToken(token: String) {
        val encrypted = cipher.encrypt(token)
        dataStore.edit { it[KEY_TOKEN] = encrypted }
        this.token = Cached.Known(token)
    }

    override suspend fun clear() {
        token = Cached.Known(null)
        dataStore.edit { it.remove(KEY_TOKEN) }
        cleaner.clear()
    }

    override fun deviceId(): String = deviceId ?: runBlocking { loadDeviceId() }

    private suspend fun loadDeviceId(): String {
        var id: String? = null
        dataStore.edit { prefs ->
            id = prefs[KEY_DEVICE_ID] ?: UUID.randomUUID().toString().also { prefs[KEY_DEVICE_ID] = it }
        }
        return checkNotNull(id).also { deviceId = it }
    }

    private companion object {
        val KEY_TOKEN = stringPreferencesKey("session_token")
        val KEY_DEVICE_ID = stringPreferencesKey("device_id")
    }
}
