package app.quoker.feature.home.navigation

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import app.quoker.feature.home.HomeScreen
import kotlinx.serialization.Serializable

/** Main screen; the start destination while signed in. */
@Serializable
data object HomeRoute

/**
 * Registers [HomeRoute].
 *
 * - [onOpenVideo]: a validated link (LinkExtractor's canonical `Link.url`) from the field, or the
 *   `Video.canonicalUrl` of a «Недавние» item. Always a URL, never a video id: open the video sheet
 *   with it (the core resolves known videos from its cache).
 * - [onOpenHistory]: «Вся история».
 * - [onOpenSettings]: the gear in the top bar.
 */
fun NavGraphBuilder.homeScreen(
    onOpenVideo: (url: String) -> Unit,
    onOpenHistory: () -> Unit,
    onOpenSettings: () -> Unit,
) {
    composable<HomeRoute> {
        HomeScreen(onOpenVideo = onOpenVideo, onOpenHistory = onOpenHistory, onOpenSettings = onOpenSettings)
    }
}
