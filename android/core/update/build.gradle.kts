plugins {
    alias(libs.plugins.quoker.android.library)
    alias(libs.plugins.quoker.android.compose)
    alias(libs.plugins.quoker.android.hilt)
}

android {
    namespace = "app.quoker.core.update"
}

dependencies {
    api(project(":core:common"))
    api(project(":core:data"))
    implementation(project(":core:designsystem"))
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.kotlinx.coroutines.android)

    testImplementation(libs.okhttp.mockwebserver)
}
