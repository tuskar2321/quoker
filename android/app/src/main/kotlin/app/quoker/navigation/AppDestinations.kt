package app.quoker.navigation

import android.content.Context
import android.content.Intent
import app.quoker.MainActivity
import app.quoker.feature.home.navigation.HomeRoute
import app.quoker.feature.onboarding.navigation.InviteRoute

/** Screens other entry points (ShareActivity) can ask MainActivity to open. */
enum class PendingDestination { TelegramLink, Invite }

object MainIntents {
    const val EXTRA_DESTINATION = "app.quoker.extra.DESTINATION"

    /** Opens (or brings back) MainActivity in the app's own task, optionally at [destination]. */
    fun open(context: Context, destination: PendingDestination? = null): Intent =
        Intent(context, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
            .apply { destination?.let { putExtra(EXTRA_DESTINATION, it.name) } }

    fun destination(intent: Intent?): PendingDestination? {
        val name = intent?.getStringExtra(EXTRA_DESTINATION) ?: return null
        return PendingDestination.entries.firstOrNull { it.name == name }
    }
}

/** The NavHost start destination: the session decides between onboarding and the main screen. */
fun startDestination(signedIn: Boolean): Any = if (signedIn) HomeRoute else InviteRoute
