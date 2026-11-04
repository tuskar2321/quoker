package app.quoker.feature.onboarding.invite

import android.os.Build
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent

/** Device name sent with the registration (shown to the admin in the user list). */
fun interface DeviceNameProvider {
    fun deviceName(): String
}

@Module
@InstallIn(SingletonComponent::class)
internal object DeviceNameModule {
    @Provides
    fun provideDeviceName(): DeviceNameProvider = DeviceNameProvider {
        Build.MODEL?.takeIf { it.isNotBlank() } ?: "Android"
    }
}
