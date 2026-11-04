package app.quoker.feature.settings.navigation

import androidx.navigation.NavController
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavOptions
import androidx.navigation.compose.composable
import app.quoker.feature.settings.SettingsScreen
import app.quoker.feature.settings.admin.AdminStatusScreen
import app.quoker.feature.settings.admin.InvitesScreen
import app.quoker.feature.settings.admin.UsersScreen
import app.quoker.feature.settings.admin.WorkersScreen
import kotlinx.serialization.Serializable

@Serializable
data object SettingsRoute

/** «Администрирование» (status); reachable from Settings for role = admin only. */
@Serializable
data object AdminRoute

@Serializable
data object AdminInvitesRoute

@Serializable
data object AdminUsersRoute

@Serializable
data object AdminWorkersRoute

fun NavController.navigateToSettings(navOptions: NavOptions? = null) = navigate(SettingsRoute, navOptions)

/**
 * Registers Settings and the admin screens; navigation between them stays inside this graph.
 *
 * - [onBack]: back arrow of Settings.
 * - [onLinkTelegram]: «Привязать Telegram» (Settings reloads the account when it resumes).
 * - [onCheckUpdates]: «Проверить обновления».
 * - [onSignedOut]: the session is already cleared («Выйти» confirmed); go to onboarding.
 */
fun NavGraphBuilder.settingsGraph(
    navController: NavController,
    onBack: () -> Unit,
    onLinkTelegram: () -> Unit,
    onCheckUpdates: () -> Unit,
    onSignedOut: () -> Unit,
) {
    val popBack: () -> Unit = { navController.popBackStack() }
    composable<SettingsRoute> {
        SettingsScreen(
            onBack = onBack,
            onLinkTelegram = onLinkTelegram,
            onOpenAdmin = { navController.navigate(AdminRoute) { launchSingleTop = true } },
            onCheckUpdates = onCheckUpdates,
            onSignedOut = onSignedOut,
        )
    }
    composable<AdminRoute> {
        AdminStatusScreen(
            onBack = popBack,
            onOpenInvites = { navController.navigate(AdminInvitesRoute) { launchSingleTop = true } },
            onOpenUsers = { navController.navigate(AdminUsersRoute) { launchSingleTop = true } },
            onOpenWorkers = { navController.navigate(AdminWorkersRoute) { launchSingleTop = true } },
        )
    }
    composable<AdminInvitesRoute> { InvitesScreen(onBack = popBack) }
    composable<AdminUsersRoute> { UsersScreen(onBack = popBack) }
    composable<AdminWorkersRoute> { WorkersScreen(onBack = popBack) }
}
