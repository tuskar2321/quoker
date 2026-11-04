package app.quoker.core.download

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import androidx.work.Constraints
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.OutOfQuotaPolicy
import androidx.work.WorkInfo
import androidx.work.WorkManager
import androidx.work.workDataOf
import app.quoker.core.download.DownloadWorker.Companion.toError
import app.quoker.core.download.internal.DownloadNotifier
import app.quoker.core.download.internal.DownloadPipeline
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map

/** Phone downloads, one unique WorkManager job per video id. */
interface VideoDownloads {
    /** Starts (or keeps the running) download; [title] is shown in the notification («@author»). */
    fun start(videoId: String, title: String?)

    fun cancel(videoId: String)

    fun observe(videoId: String): Flow<DownloadState>

    /**
     * Android 13+: POST_NOTIFICATIONS is not granted and was not asked yet. Ask on the first
     * download, then call [onNotificationPermissionAsked]; the download works without it.
     */
    fun needsNotificationPermission(): Boolean

    fun onNotificationPermissionAsked()

    /** Download notifications can be shown (permission granted or not needed, channel on). */
    fun notificationsAllowed(): Boolean

    /** API 26–28: WRITE_EXTERNAL_STORAGE is required before [start]. */
    fun needsStoragePermission(): Boolean

    companion object {
        const val NOTIFICATION_PERMISSION = "android.permission.POST_NOTIFICATIONS"
        const val STORAGE_PERMISSION = Manifest.permission.WRITE_EXTERNAL_STORAGE

        /** ACTION_VIEW for a [DownloadState.Saved] uri (type video/\*, read grant). */
        fun openSavedVideoIntent(uri: String): Intent = DownloadNotifier.openVideoIntent(Uri.parse(uri))
    }
}

@Singleton
internal class WorkManagerVideoDownloads @Inject constructor(
    @ApplicationContext private val context: Context,
    private val pipeline: DownloadPipeline,
) : VideoDownloads {
    // Resolved lazily: the app configures WorkManager (HiltWorkerFactory) on first use.
    private val workManager: WorkManager get() = WorkManager.getInstance(context)
    private val prefs by lazy { context.getSharedPreferences(PREFS, Context.MODE_PRIVATE) }

    override fun start(videoId: String, title: String?) {
        val request = OneTimeWorkRequestBuilder<DownloadWorker>()
            .setInputData(workDataOf(DownloadWorker.KEY_VIDEO_ID to videoId, DownloadWorker.KEY_TITLE to title))
            .setExpedited(OutOfQuotaPolicy.RUN_AS_NON_EXPEDITED_WORK_REQUEST)
            .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
            .addTag(TAG)
            .build()
        // KEEP: a running download continues; a finished or failed one is replaced.
        workManager.enqueueUniqueWork(uniqueName(videoId), ExistingWorkPolicy.KEEP, request)
    }

    override fun cancel(videoId: String) {
        workManager.cancelUniqueWork(uniqueName(videoId))
        pipeline.discard(videoId)
    }

    override fun observe(videoId: String): Flow<DownloadState> =
        workManager.getWorkInfosForUniqueWorkFlow(uniqueName(videoId))
            .map { infos -> latest(infos).toDownloadState() }
            .distinctUntilChanged()

    override fun needsNotificationPermission(): Boolean =
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            !granted(NOTIFICATION_PERMISSION_33) &&
            !prefs.getBoolean(KEY_NOTIFICATIONS_ASKED, false)

    override fun onNotificationPermissionAsked() {
        prefs.edit().putBoolean(KEY_NOTIFICATIONS_ASKED, true).apply()
    }

    override fun notificationsAllowed(): Boolean =
        (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU || granted(NOTIFICATION_PERMISSION_33)) &&
            NotificationManagerCompat.from(context).areNotificationsEnabled()

    override fun needsStoragePermission(): Boolean =
        Build.VERSION.SDK_INT <= Build.VERSION_CODES.P && !granted(Manifest.permission.WRITE_EXTERNAL_STORAGE)

    private fun granted(permission: String) =
        ContextCompat.checkSelfPermission(context, permission) == PackageManager.PERMISSION_GRANTED

    companion object {
        const val TAG = "quoker-download"
        private const val PREFS = "quoker_downloads"
        private const val KEY_NOTIFICATIONS_ASKED = "notifications_asked"
        private const val NOTIFICATION_PERMISSION_33 = VideoDownloads.NOTIFICATION_PERMISSION

        fun uniqueName(videoId: String) = "download:$videoId"
    }
}

/** The unfinished job if any, else the most recent one. */
internal fun latest(infos: List<WorkInfo>): WorkInfo? =
    infos.firstOrNull { !it.state.isFinished } ?: infos.lastOrNull()

internal fun WorkInfo?.toDownloadState(): DownloadState = when (this?.state) {
    null, WorkInfo.State.CANCELLED -> DownloadState.Idle
    WorkInfo.State.ENQUEUED, WorkInfo.State.BLOCKED -> DownloadState.Preparing
    WorkInfo.State.RUNNING -> when (progress.getString(DownloadWorker.KEY_PHASE)) {
        DownloadWorker.PHASE_DOWNLOADING -> DownloadState.Downloading(progress.getInt(DownloadWorker.KEY_PERCENT, 0))
        else -> DownloadState.Preparing
    }
    WorkInfo.State.SUCCEEDED -> outputData.getString(DownloadWorker.KEY_URI)?.let(DownloadState::Saved) ?: DownloadState.Idle
    WorkInfo.State.FAILED -> DownloadState.Failed(outputData.toError())
}
