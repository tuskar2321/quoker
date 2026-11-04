plugins {
    alias(libs.plugins.quoker.android.library)
    alias(libs.plugins.quoker.android.hilt)
}

android {
    namespace = "app.quoker.core.download"
}

dependencies {
    api(project(":core:common"))
    implementation(project(":core:data"))
    // Notification icon and shared strings (error_<code>, download_progress).
    implementation(project(":core:designsystem"))
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.work.runtime.ktx)
    implementation(libs.androidx.hilt.work)
    ksp(libs.androidx.hilt.compiler)
    implementation(libs.kotlinx.coroutines.android)

    testImplementation(libs.androidx.work.testing)
    testImplementation(libs.okhttp.mockwebserver)
}
