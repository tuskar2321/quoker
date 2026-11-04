package app.quoker

import android.content.Context
import android.content.Intent
import android.graphics.Color
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.core.update.UpdateManager
import app.quoker.core.update.UpdateState
import app.quoker.navigation.MainIntents
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    override fun attachBaseContext(newBase: Context) {
        super.attachBaseContext(AppLocale.wrap(newBase))
    }

    @Inject
    lateinit var updateManager: UpdateManager

    private val viewModel: MainViewModel by viewModels()

    // Set when «Установить» sent the user to the «install unknown apps» settings screen.
    private var awaitingInstallPermission = false

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge(
            statusBarStyle = SystemBarStyle.dark(Color.TRANSPARENT),
            navigationBarStyle = SystemBarStyle.dark(Color.TRANSPARENT),
        )
        super.onCreate(savedInstanceState)
        if (savedInstanceState == null) viewModel.onDestination(MainIntents.destination(intent))
        updateManager.checkOnStart()
        setContent {
            QuokerTheme {
                MainScreen(viewModel = viewModel, updateManager = updateManager, onUpdate = ::proceedUpdate)
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        viewModel.onDestination(MainIntents.destination(intent))
    }

    override fun onResume() {
        super.onResume()
        // Back from «Разрешить установку»: continue only if the permission was granted, otherwise
        // the installer's own confirmation (which also pauses us) would relaunch in a loop.
        if (awaitingInstallPermission && updateManager.state.value is UpdateState.ReadyToInstall) {
            awaitingInstallPermission = false
            if (packageManager.canRequestPackageInstalls()) updateManager.install(this)
        }
    }

    private fun proceedUpdate() {
        if (updateManager.state.value is UpdateState.ReadyToInstall && !packageManager.canRequestPackageInstalls()) {
            awaitingInstallPermission = true
        }
        updateManager.proceed(this)
    }
}
