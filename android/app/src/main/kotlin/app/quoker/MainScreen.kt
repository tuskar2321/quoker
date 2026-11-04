package app.quoker

import android.widget.Toast
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavHostController
import androidx.navigation.NavDestination.Companion.hasRoute
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.rememberNavController
import app.quoker.core.common.QuokerError
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.core.update.UpdateCheckResult
import app.quoker.core.update.UpdateManager
import app.quoker.core.update.ui.UpdateDialog
import app.quoker.core.update.ui.UpgradeRequiredScreen
import app.quoker.core.update.ui.updateErrorMessage
import app.quoker.feature.history.navigation.historyScreen
import app.quoker.feature.history.navigation.navigateToHistory
import app.quoker.feature.home.navigation.HomeRoute
import app.quoker.feature.home.navigation.homeScreen
import app.quoker.feature.onboarding.navigation.InviteRoute
import app.quoker.feature.onboarding.navigation.TelegramLinkRoute
import app.quoker.feature.onboarding.navigation.onboardingGraph
import app.quoker.feature.send.SendSheet
import app.quoker.feature.settings.navigation.navigateToSettings
import app.quoker.feature.settings.navigation.settingsGraph
import app.quoker.feature.video.VideoSheet
import app.quoker.feature.video.VideoSource
import app.quoker.navigation.PendingDestination
import app.quoker.navigation.startDestination
import app.quoker.ui.BlockedScreen
import kotlinx.coroutines.launch
import app.quoker.core.update.R as UpdateR

private sealed interface UpdateCheckMessage {
    data object UpToDate : UpdateCheckMessage

    data class Failed(val error: QuokerError) : UpdateCheckMessage
}

@Composable
internal fun MainScreen(viewModel: MainViewModel, updateManager: UpdateManager, onUpdate: () -> Unit) {
    val signedIn by viewModel.signedIn.collectAsStateWithLifecycle()
    val blocked by viewModel.blocked.collectAsStateWithLifecycle()
    val update by updateManager.state.collectAsStateWithLifecycle()
    val sheets by viewModel.sheets.collectAsStateWithLifecycle()
    val pending by viewModel.pending.collectAsStateWithLifecycle()
    val navController = rememberNavController()
    val scope = rememberCoroutineScope()
    var checkMessage by remember { mutableStateOf<UpdateCheckMessage?>(null) }

    // Blank themed background until the session is known: no splash, no wrong first screen.
    Box(Modifier.fillMaxSize().background(QuokerTheme.colors.background)) {
        val known = signedIn ?: return@Box
        val startSignedIn = rememberSaveable { known }
        val overlay = update.required || blocked

        NavHost(
            navController = navController,
            startDestination = remember { startDestination(startSignedIn) },
            modifier = if (overlay) Modifier.fillMaxSize().clearAndSetSemantics {} else Modifier.fillMaxSize(),
        ) {
            onboardingGraph(
                onSignedIn = { linked ->
                    if (linked) navController.navigateClearing(HomeRoute)
                    else navController.navigateClearing(TelegramLinkRoute(showSkip = true))
                },
                onTelegramLinked = { route ->
                    if (route.showSkip) navController.navigateClearing(HomeRoute) else navController.popBackStack()
                },
                onTelegramSkipped = { navController.navigateClearing(HomeRoute) },
                onBack = { navController.popBackStack() },
            )
            homeScreen(
                onOpenVideo = { url -> viewModel.openVideo(VideoSource.Url(url)) },
                onOpenHistory = { navController.navigateToHistory() },
                onOpenSettings = { navController.navigateToSettings() },
            )
            historyScreen(
                onBack = { navController.popBackStack() },
                onOpenVideo = { id -> viewModel.openVideo(VideoSource.Id(id)) },
                // One download UI: the sheet owns permissions, progress and «Сохранено · Открыть».
                onDownload = { id -> viewModel.openVideo(VideoSource.Id(id)) },
                onSend = viewModel::openSend,
            )
            settingsGraph(
                navController = navController,
                onBack = { navController.popBackStack() },
                onLinkTelegram = { navController.navigate(TelegramLinkRoute(showSkip = false)) { launchSingleTop = true } },
                onCheckUpdates = {
                    scope.launch {
                        checkMessage = when (val result = updateManager.checkNow()) {
                            UpdateCheckResult.UpToDate -> UpdateCheckMessage.UpToDate
                            is UpdateCheckResult.Failed -> UpdateCheckMessage.Failed(result.error)
                            is UpdateCheckResult.Found -> null // UpdateManager.state opens the dialog.
                        }
                    }
                },
                onSignedOut = { navController.navigateToInvite() },
            )
        }

        LaunchedEffect(signedIn) {
            if (signedIn == false) navController.navigateToInvite()
        }
        LaunchedEffect(pending, signedIn) {
            when (pending) {
                PendingDestination.TelegramLink -> if (signedIn == true) {
                    navController.navigate(TelegramLinkRoute(showSkip = false)) { launchSingleTop = true }
                }
                PendingDestination.Invite, null -> Unit // Signed out already starts at the invite.
            }
            viewModel.consumePending()
        }

        if (!overlay) {
            val onLinkTelegram = {
                viewModel.closeSheets()
                navController.navigate(TelegramLinkRoute(showSkip = false)) { launchSingleTop = true }
            }
            sheets.video?.let { source ->
                VideoSheet(
                    source = source,
                    onSend = viewModel::openSend,
                    onLinkTelegram = onLinkTelegram,
                    onDismiss = viewModel::closeSheets,
                )
            }
            sheets.sendVideoId?.let { videoId ->
                SendSheet(
                    videoId = videoId,
                    onSent = viewModel::closeSend,
                    onLinkTelegram = onLinkTelegram,
                    onDismiss = viewModel::closeSend,
                )
            }
            UpdateDialog(state = update, onUpdate = onUpdate, onLater = updateManager::dismiss)
        }

        val blockTouches = Modifier.fillMaxSize().pointerInput(Unit) {
            awaitPointerEventScope {
                while (true) awaitPointerEvent().changes.forEach { it.consume() }
            }
        }
        // A full-screen state replaces whatever was being typed: drop focus so the keyboard closes.
        val focusManager = LocalFocusManager.current
        val fullScreenState = update.required || blocked
        LaunchedEffect(fullScreenState) {
            if (fullScreenState) focusManager.clearFocus(force = true)
        }
        when {
            update.required -> UpgradeRequiredScreen(state = update, onUpdate = onUpdate, modifier = blockTouches)
            blocked -> BlockedScreen(onSignOut = viewModel::signOut, modifier = blockTouches)
        }
    }

    checkMessage?.let { message ->
        val context = LocalContext.current
        val text = when (message) {
            UpdateCheckMessage.UpToDate -> stringResource(UpdateR.string.update_up_to_date)
            is UpdateCheckMessage.Failed -> updateErrorMessage(message.error)
        }
        LaunchedEffect(message) {
            Toast.makeText(context, text, Toast.LENGTH_SHORT).show()
            checkMessage = null
        }
    }
}

/** Replaces the whole back stack with [route] (onboarding finished, session lost). */
private fun NavHostController.navigateClearing(route: Any) {
    navigate(route) {
        popUpTo(graph.id) { inclusive = true }
        launchSingleTop = true
    }
}

private fun NavHostController.navigateToInvite() {
    if (currentDestination?.hasRoute<InviteRoute>() == true) return
    navigateClearing(InviteRoute)
}
