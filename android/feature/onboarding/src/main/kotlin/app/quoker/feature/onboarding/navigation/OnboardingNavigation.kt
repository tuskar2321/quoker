package app.quoker.feature.onboarding.navigation

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import androidx.navigation.toRoute
import app.quoker.feature.onboarding.invite.InviteScreen
import app.quoker.feature.onboarding.telegram.TelegramLinkScreen
import kotlinx.serialization.Serializable

/** Invite code entry; the start destination while signed out. */
@Serializable
data object InviteRoute

/**
 * Telegram binding. [showSkip] = true in onboarding (shows «Пропустить», no back arrow); false when
 * opened from Settings or the video sheet («Нет Telegram»), which shows a back arrow instead.
 */
@Serializable
data class TelegramLinkRoute(val showSkip: Boolean = false)

/**
 * Registers [InviteRoute] and [TelegramLinkRoute].
 *
 * - [onSignedIn]: the session is stored; `telegramLinked` tells whether the account already has
 *   Telegram (then the link screen can be skipped).
 * - [onTelegramLinked]: binding confirmed; the route tells where the screen was opened from.
 * - [onTelegramSkipped]: «Пропустить» (only when `showSkip`).
 * - [onBack]: back arrow of the link screen (only when not `showSkip`).
 */
fun NavGraphBuilder.onboardingGraph(
    onSignedIn: (telegramLinked: Boolean) -> Unit,
    onTelegramLinked: (TelegramLinkRoute) -> Unit,
    onTelegramSkipped: () -> Unit,
    onBack: () -> Unit,
) {
    composable<InviteRoute> {
        InviteScreen(onSignedIn = onSignedIn)
    }
    composable<TelegramLinkRoute> { entry ->
        val route = entry.toRoute<TelegramLinkRoute>()
        TelegramLinkScreen(
            showSkip = route.showSkip,
            onLinked = { onTelegramLinked(route) },
            onSkip = onTelegramSkipped,
            onBack = onBack,
        )
    }
}
