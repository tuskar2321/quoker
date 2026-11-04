package app.quoker.navigation

import android.content.Intent
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.quoker.MainActivity
import app.quoker.feature.home.navigation.HomeRoute
import app.quoker.feature.onboarding.navigation.InviteRoute
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

@RunWith(AndroidJUnit4::class)
@Config(application = android.app.Application::class)
class AppDestinationsTest {
    private val context = ApplicationProvider.getApplicationContext<android.content.Context>()

    @Test
    fun sessionPicksStartDestination() {
        assertEquals(HomeRoute, startDestination(signedIn = true))
        assertEquals(InviteRoute, startDestination(signedIn = false))
    }

    @Test
    fun mainIntentCarriesPendingDestination() {
        for (destination in PendingDestination.entries) {
            val intent = MainIntents.open(context, destination)
            assertEquals(MainActivity::class.java.name, intent.component?.className)
            assertTrue(intent.flags and Intent.FLAG_ACTIVITY_NEW_TASK != 0)
            assertEquals(destination, MainIntents.destination(intent))
        }
    }

    @Test
    fun unknownOrMissingDestinationIsIgnored() {
        assertNull(MainIntents.destination(null))
        assertNull(MainIntents.destination(MainIntents.open(context)))
        assertNull(MainIntents.destination(Intent().putExtra(MainIntents.EXTRA_DESTINATION, "admin")))
    }
}
