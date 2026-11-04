package app.quoker.core.update

import android.app.Activity
import app.quoker.core.data.model.AppVersion
import app.quoker.core.data.model.AppVersionCheck
import app.quoker.core.data.repository.AppVersionRepository
import app.quoker.core.network.QuokerException
import app.quoker.core.update.install.ApkInstaller
import java.io.File
import java.security.MessageDigest

internal const val INSTALLED_VERSION = 10

internal fun sha256Hex(bytes: ByteArray): String =
    MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }

internal fun release(
    versionCode: Int = 11,
    minSupported: Int = 1,
    apkUrl: String = "/download/quoker-1.1.apk",
    sha256: String = "00",
) = AppVersion(versionCode, "1.$versionCode", apkUrl, sha256, minSupported, "Новое")

/** Mirrors AppVersionRepositoryImpl: daily check returns null within 24 h of the last success. */
internal class FakeAppVersionRepository(
    var latest: AppVersion? = null,
    var error: QuokerException? = null,
) : AppVersionRepository {
    var checkCalls = 0
    var dailyCalls = 0
    var lastCheckAtMs: Long? = null
    var nowMs: Long = 0

    override suspend fun check(): AppVersionCheck? {
        checkCalls++
        error?.let { throw it }
        lastCheckAtMs = nowMs
        return latest?.let {
            AppVersionCheck(
                latest = it,
                updateAvailable = it.versionCode > INSTALLED_VERSION,
                upgradeRequired = INSTALLED_VERSION < it.minSupportedVersionCode,
            )
        }
    }

    override suspend fun checkDaily(): AppVersionCheck? {
        dailyCalls++
        val last = lastCheckAtMs
        if (last != null && nowMs - last in 0 until DAY_MS) return null
        return check()
    }

    companion object {
        const val DAY_MS = 24L * 60 * 60 * 1000
    }
}

internal class FakeInstaller(var allowed: Boolean = true) : ApkInstaller {
    val installed = mutableListOf<File>()
    var permissionRequests = 0

    override fun canInstall(): Boolean = allowed

    override fun requestPermission(activity: Activity) {
        permissionRequests++
    }

    override fun install(apk: File) {
        installed += apk
    }
}
