package app.quoker.core.update.install

import android.app.Activity
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.net.Uri
import android.provider.Settings
import dagger.hilt.android.qualifiers.ApplicationContext
import java.io.File
import javax.inject.Inject

internal interface ApkInstaller {
    /** «Install unknown apps» is granted for this package (API 26+ always asks). */
    fun canInstall(): Boolean

    /** Opens the system «Разрешить установку из этого источника» screen. */
    fun requestPermission(activity: Activity)

    /** Hands [apk] to the system installer; status arrives at [InstallStatusReceiver]. Blocking IO. */
    fun install(apk: File)
}

/**
 * PackageInstaller session rather than ACTION_VIEW + FileProvider: it reads the APK from our
 * private cacheDir without exposing a content URI to another app, reports success/failure/abort
 * back to us, and is the non-deprecated path (ACTION_INSTALL_PACKAGE is deprecated since API 29).
 */
internal class SessionApkInstaller @Inject constructor(
    @ApplicationContext private val context: Context,
) : ApkInstaller {
    override fun canInstall(): Boolean = context.packageManager.canRequestPackageInstalls()

    override fun requestPermission(activity: Activity) {
        activity.startActivity(
            Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}")),
        )
    }

    override fun install(apk: File) {
        val installer = context.packageManager.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL).apply {
            setAppPackageName(context.packageName)
            setSize(apk.length())
        }
        val sessionId = installer.createSession(params)
        try {
            installer.openSession(sessionId).use { session ->
                session.openWrite("base.apk", 0, apk.length()).use { out ->
                    apk.inputStream().use { it.copyTo(out) }
                    session.fsync(out)
                }
                val intent = Intent(context, InstallStatusReceiver::class.java)
                    .setAction(InstallStatusReceiver.ACTION_STATUS)
                // Mutable: the installer fills in the status extras.
                val pending = PendingIntent.getBroadcast(
                    context,
                    sessionId,
                    intent,
                    PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE,
                )
                session.commit(pending.intentSender)
            }
        } catch (e: Exception) {
            runCatching { installer.abandonSession(sessionId) }
            throw e
        }
    }
}
