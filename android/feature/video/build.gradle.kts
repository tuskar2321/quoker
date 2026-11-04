plugins {
    alias(libs.plugins.quoker.android.feature)
}

android {
    namespace = "app.quoker.feature.video"
}

dependencies {
    implementation(project(":core:download"))
    implementation(libs.androidx.activity.compose)
}
