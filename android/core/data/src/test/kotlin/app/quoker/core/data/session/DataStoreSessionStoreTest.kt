package app.quoker.core.data.session

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import app.cash.turbine.test
import app.quoker.core.data.FakeTokenCipher
import app.quoker.core.network.SessionEvent
import app.quoker.core.network.SessionEvents
import java.io.File
import java.nio.file.Files
import java.util.UUID
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

// Real DataStore on disk with real dispatchers: the provider methods block with runBlocking,
// which must not share a thread with DataStore's own scope.
class DataStoreSessionStoreTest {
    private val dir: File = Files.createTempDirectory("session").toFile()
    private val scopes = mutableListOf<CoroutineScope>()
    private val events = SessionEvents()
    private var cleanerCalls = 0

    @After
    fun tearDown() {
        scopes.forEach { it.cancel() }
        dir.deleteRecursively()
    }

    private fun newScope() = CoroutineScope(SupervisorJob() + Dispatchers.IO).also { scopes += it }

    private fun dataStore(name: String = "session"): DataStore<Preferences> =
        PreferenceDataStoreFactory.create(scope = newScope()) { File(dir, "$name.preferences_pb") }

    private fun store(ds: DataStore<Preferences>) =
        DataStoreSessionStore(ds, FakeTokenCipher(), { cleanerCalls++ }, events, newScope())

    @Test
    fun tokenIsStoredEncryptedAndReadBack() = runBlocking {
        val ds = dataStore()
        val s = store(ds)
        assertNull(s.sessionToken())
        assertFalse(s.signedIn.first())

        s.saveToken("secret")
        assertEquals("secret", s.sessionToken())
        assertTrue(s.signedIn.first())
        val raw = ds.data.first()[stringPreferencesKey("session_token")]
        assertEquals("enc:terces", raw)

        // A fresh instance (new process) decrypts from disk.
        assertEquals("secret", store(ds).sessionToken())
    }

    @Test
    fun deviceIdIsGeneratedOnceAndSurvivesSignOut() = runBlocking {
        val ds = dataStore()
        val s = store(ds)
        val id = s.deviceId()
        UUID.fromString(id)
        assertEquals(id, s.deviceId())
        s.saveToken("t")
        s.clear()
        assertEquals(id, store(ds).deviceId())
        assertNotEquals(id, store(dataStore("other")).deviceId())
    }

    @Test
    fun clearDropsTokenAndCaches() = runBlocking {
        val s = store(dataStore())
        s.saveToken("t")
        s.clear()
        assertNull(s.sessionToken())
        assertFalse(s.signedIn.first())
        assertEquals(1, cleanerCalls)
    }

    @Test
    fun signedOutEventClearsSession() = runBlocking {
        val s = store(dataStore())
        s.saveToken("t")
        s.signedIn.test {
            assertTrue(awaitItem())
            events.emit(SessionEvent.SignedOut)
            assertFalse(awaitItem())
        }
        assertNull(s.sessionToken())
    }

    @Test
    fun otherEventsKeepSession() = runBlocking {
        val s = store(dataStore())
        s.saveToken("t")
        events.emit(SessionEvent.Blocked)
        events.emit(SessionEvent.UpgradeRequired)
        withTimeout(1_000) { assertTrue(s.signedIn.first()) }
        assertEquals("t", s.sessionToken())
    }

    @Test
    fun undecryptableTokenMeansSignedOut() = runBlocking {
        val ds = dataStore()
        ds.edit { it[stringPreferencesKey("session_token")] = "garbage" }
        val s = store(ds)
        assertNull(s.sessionToken())
        assertFalse(s.signedIn.first())
    }
}
