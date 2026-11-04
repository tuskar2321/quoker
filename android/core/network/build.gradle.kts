plugins {
    alias(libs.plugins.quoker.android.library)
    alias(libs.plugins.quoker.android.hilt)
    alias(libs.plugins.quoker.kotlin.serialization)
}

android {
    namespace = "app.quoker.core.network"
}

dependencies {
    api(project(":core:common"))
    api(libs.okhttp)
    api(libs.retrofit)
    api(libs.kotlinx.serialization.json)
    api(libs.kotlinx.coroutines.core)
    implementation(libs.retrofit.kotlinx.serialization)

    testImplementation(libs.okhttp.mockwebserver)
}
