plugins {
    alias(libs.plugins.quoker.android.library)
    alias(libs.plugins.quoker.android.hilt)
}

android {
    namespace = "app.quoker.core.common"
}

dependencies {
    api(libs.kotlinx.coroutines.core)
    testImplementation(libs.kotlinx.serialization.json)
}
