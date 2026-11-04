package app.quoker

import android.content.ComponentName
import android.content.Intent
import android.content.pm.ActivityInfo
import android.content.pm.PackageManager
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

@RunWith(AndroidJUnit4::class)
@Config(application = android.app.Application::class)
class ManifestTest {
    private val context = ApplicationProvider.getApplicationContext<android.content.Context>()

    @Test
    fun shareActivityOpensOverOtherAppsAndStaysOutOfRecents() {
        val info = context.packageManager.getActivityInfo(
            ComponentName(context, ShareActivity::class.java),
            PackageManager.GET_META_DATA,
        )
        assertTrue(info.flags and ActivityInfo.FLAG_EXCLUDE_FROM_RECENTS != 0)
        assertNotEquals(context.packageName, info.taskAffinity)
        assertEquals(ActivityInfo.SCREEN_ORIENTATION_PORTRAIT, info.screenOrientation)
    }

    @Test
    fun shareActivityHandlesPlainTextSend() {
        val intent = Intent(Intent.ACTION_SEND).setType("text/plain").setPackage(context.packageName)
        val resolved = context.packageManager.queryIntentActivities(intent, 0)
        assertEquals(ShareActivity::class.java.name, resolved.single().activityInfo.name)
    }

    @Test
    fun debugBuildTargetsEmulatorHost() {
        assertTrue(BuildConfig.API_BASE_URL.startsWith("http://10.0.2.2"))
    }
}
