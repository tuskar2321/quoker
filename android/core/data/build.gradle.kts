plugins {
    alias(libs.plugins.quoker.android.library)
    alias(libs.plugins.quoker.android.hilt)
    alias(libs.plugins.quoker.android.room)
    alias(libs.plugins.quoker.kotlin.serialization)
}

android {
    namespace = "app.quoker.core.data"
}

dependencies {
    api(project(":core:common"))
    api(project(":core:network"))
    implementation(libs.androidx.datastore.preferences)
    implementation(libs.tink.android)
    implementation(libs.kotlinx.coroutines.android)

    testImplementation(libs.okhttp.mockwebserver)
}
