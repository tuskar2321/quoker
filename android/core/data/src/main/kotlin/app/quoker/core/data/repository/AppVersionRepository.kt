package app.quoker.core.data.repository

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.longPreferencesKey
import app.quoker.core.common.AppInfo
import app.quoker.core.common.Clock
import app.quoker.core.data.di.AppPreferences
import app.quoker.core.data.model.AppVersionCheck
import app.quoker.core.data.model.toDomain
import app.quoker.core.network.ErrorCodes
import app.quoker.core.network.QuokerApi
import app.quoker.core.network.QuokerException
import app.quoker.core.network.quokerCall
import java.time.Duration
import javax.inject.Inject
import kotlinx.coroutines.flow.first

interface AppVersionRepository {
    /**
     * GET /v1/app/version compared with the installed version. Null when no release is
     * published yet. For «Проверить обновления».
     */
    suspend fun check(): AppVersionCheck?

    /**
     * Same as [check], but at most once per day (for app start): returns null without a request
     * when the last successful check was less than 24 h ago.
     */
    suspend fun checkDaily(): AppVersionCheck?
}

internal class AppVersionRepositoryImpl @Inject constructor(
    private val api: QuokerApi,
    private val appInfo: AppInfo,
    private val clock: Clock,
    @AppPreferences private val prefs: DataStore<Preferences>,
) : AppVersionRepository {
    override suspend fun check(): AppVersionCheck? {
        val latest = try {
            quokerCall { api.appVersion() }.toDomain()
        } catch (e: QuokerException) {
            if (e.code == ErrorCodes.NOT_FOUND) null else throw e
        }
        prefs.edit { it[KEY_LAST_CHECK] = clock.now().toEpochMilli() }
        return latest?.let {
            AppVersionCheck(
                latest = it,
                updateAvailable = it.versionCode > appInfo.versionCode,
                upgradeRequired = appInfo.versionCode < it.minSupportedVersionCode,
            )
        }
    }

    override suspend fun checkDaily(): AppVersionCheck? {
        val last = prefs.data.first()[KEY_LAST_CHECK]
        val now = clock.now().toEpochMilli()
        // A clock moved backwards also makes the check due.
        if (last != null && now - last in 0 until CHECK_INTERVAL_MS) return null
        return check()
    }

    private companion object {
        val KEY_LAST_CHECK = longPreferencesKey("app_version_last_check_ms")
        val CHECK_INTERVAL_MS = Duration.ofDays(1).toMillis()
    }
}
