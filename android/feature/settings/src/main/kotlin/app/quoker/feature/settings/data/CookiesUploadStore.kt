package app.quoker.feature.settings.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.longPreferencesKey
import app.quoker.core.data.di.AppPreferences
import java.time.Instant
import javax.inject.Inject
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

/**
 * When this device last uploaded Instagram cookies successfully. The core does not expose the
 * upload date, so uploads from the bot (/cookies) or another device are not seen here.
 */
interface CookiesUploadStore {
    val lastUploadAt: Flow<Instant?>

    suspend fun markUploaded(at: Instant)
}

internal class DataStoreCookiesUploadStore @Inject constructor(
    @AppPreferences private val dataStore: DataStore<Preferences>,
) : CookiesUploadStore {
    override val lastUploadAt: Flow<Instant?> =
        dataStore.data.map { prefs -> prefs[KEY]?.let(Instant::ofEpochMilli) }

    override suspend fun markUploaded(at: Instant) {
        dataStore.edit { it[KEY] = at.toEpochMilli() }
    }

    private companion object {
        val KEY = longPreferencesKey("admin_instagram_cookies_uploaded_at")
    }
}
