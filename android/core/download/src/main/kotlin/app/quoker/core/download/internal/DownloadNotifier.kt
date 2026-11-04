package app.quoker.core.download.internal

import android.Manifest
import android.annotation.SuppressLint
import android.app.Notification
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.net.Uri
import android.os.Build
import androidx.core.app.NotificationChannelCompat
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import androidx.work.ForegroundInfo
import androidx.work.WorkManager
import app.quoker.core.common.QuokerError
import app.quoker.core.download.R
import app.quoker.core.download.downloadMessage
import dagger.hilt.android.qualifiers.ApplicationContext
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton
import app.quoker.core.designsystem.R as DesignR

/** Notifications of the «Загрузки» channel: progress (foreground), «Сохранено», failure. */
@Singleton
class DownloadNotifier @Inject constructor(
    @ApplicationContext private val context: Context,
) {
    private val manager = NotificationManagerCompat.from(context)

    fun ensureChannel() {
        val channel = NotificationChannelCompat.Builder(CHANNEL_ID, NotificationManagerCompat.IMPORTANCE_LOW)
            .setName(context.getString(R.string.download_channel_name))
            .setDescription(context.getString(R.string.download_channel_description))
            .setShowBadge(false)
            .build()
        manager.createNotificationChannel(channel)
    }

    /** [percent] null: preparing (indeterminate). */
    fun progressForeground(workId: UUID, title: String?, percent: Int?): ForegroundInfo {
        ensureChannel()
        val cancel = WorkManager.getInstance(context).createCancelPendingIntent(workId)
        val text = if (percent == null) {
            context.getString(R.string.download_notification_preparing)
        } else {
            context.getString(DesignR.string.download_progress, percent)
        }
        val notification = base(title)
            .setContentText(text)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setProgress(100, percent ?: 0, percent == null)
            .addAction(0, context.getString(R.string.download_notification_cancel), cancel)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .build()
        val id = foregroundId(workId)
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            ForegroundInfo(id, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        } else {
            ForegroundInfo(id, notification)
        }
    }

    fun saved(videoId: String, title: String?, uri: String) {
        val open = PendingIntent.getActivity(
            context,
            videoId.hashCode(),
            openVideoIntent(Uri.parse(uri)),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val name = title ?: context.getString(R.string.download_notification_default_title)
        notify(
            resultId(videoId),
            base(context.getString(R.string.download_notification_saved))
                .setContentText(context.getString(R.string.download_notification_saved_text, name))
                .setContentIntent(open)
                .setAutoCancel(true)
                .build(),
        )
    }

    fun failed(videoId: String, title: String?, error: QuokerError) {
        val name = title ?: context.getString(R.string.download_notification_default_title)
        notify(
            resultId(videoId),
            base(context.getString(R.string.download_notification_failed))
                .setContentText("$name · ${error.downloadMessage(context.resources)}")
                .setAutoCancel(true)
                .build(),
        )
    }

    private fun base(title: String?) = NotificationCompat.Builder(context, CHANNEL_ID)
        .setSmallIcon(DesignR.drawable.ic_notification)
        .setContentTitle(title ?: context.getString(R.string.download_notification_default_title))
        .setCategory(NotificationCompat.CATEGORY_PROGRESS)
        .setPriority(NotificationCompat.PRIORITY_LOW)

    @SuppressLint("MissingPermission") // checked in canNotify()
    private fun notify(id: Int, notification: Notification) {
        if (!canNotify()) return
        ensureChannel()
        manager.notify(id, notification)
    }

    private fun canNotify(): Boolean {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            return false
        }
        return manager.areNotificationsEnabled()
    }

    companion object {
        const val CHANNEL_ID = "downloads"

        fun foregroundId(workId: UUID) = workId.hashCode()

        fun resultId(videoId: String) = videoId.hashCode() xor 0x51A7

        /** ACTION_VIEW for a saved MediaStore video. */
        fun openVideoIntent(uri: Uri): Intent = Intent(Intent.ACTION_VIEW)
            .setDataAndType(uri, "video/*")
            .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
    }
}
