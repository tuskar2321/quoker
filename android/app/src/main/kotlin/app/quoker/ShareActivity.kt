package app.quoker

import android.content.Context
import android.graphics.Color
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.quoker.core.data.repository.AuthRepository
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.feature.send.SendSheet
import app.quoker.feature.video.VideoSheet
import app.quoker.navigation.MainIntents
import app.quoker.navigation.PendingDestination
import app.quoker.share.ShareLinks
import app.quoker.ui.SignInFirstSheet
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject

/** ACTION_SEND text/plain: the "Видео" sheet over the source app; finishes when it is closed. */
@AndroidEntryPoint
class ShareActivity : ComponentActivity() {
    override fun attachBaseContext(newBase: Context) {
        super.attachBaseContext(AppLocale.wrap(newBase))
    }

    @Inject
    lateinit var auth: AuthRepository

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge(
            statusBarStyle = SystemBarStyle.dark(Color.TRANSPARENT),
            navigationBarStyle = SystemBarStyle.dark(Color.TRANSPARENT),
        )
        super.onCreate(savedInstanceState)
        val source = ShareLinks.source(intent)
        setContent {
            QuokerTheme {
                val signedIn by remember { auth.signedIn }.collectAsStateWithLifecycle(initialValue = null)
                var sendVideoId by rememberSaveable { mutableStateOf<String?>(null) }
                when (signedIn) {
                    null -> Unit
                    false -> SignInFirstSheet(
                        onOpenApp = { openMain(PendingDestination.Invite) },
                        onDismiss = ::finish,
                    )
                    true -> {
                        val onLinkTelegram = { openMain(PendingDestination.TelegramLink) }
                        VideoSheet(
                            source = source,
                            onSend = { sendVideoId = it },
                            onLinkTelegram = onLinkTelegram,
                            onDismiss = ::finish,
                        )
                        sendVideoId?.let { videoId ->
                            SendSheet(
                                videoId = videoId,
                                onSent = { sendVideoId = null },
                                onLinkTelegram = onLinkTelegram,
                                onDismiss = { sendVideoId = null },
                            )
                        }
                    }
                }
            }
        }
    }

    private fun openMain(destination: PendingDestination) {
        startActivity(MainIntents.open(this, destination))
        finish()
    }
}
