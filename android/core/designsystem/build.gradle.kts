plugins {
    alias(libs.plugins.quoker.android.library)
    alias(libs.plugins.quoker.android.compose)
}

android {
    namespace = "app.quoker.core.designsystem"
}

dependencies {
    api(project(":core:common"))
    implementation(libs.androidx.core.ktx)
    implementation(libs.coil.compose)
}
