plugins {
    alias(libs.plugins.quoker.android.feature)
}

android {
    namespace = "app.quoker.feature.settings"
}

dependencies {
    // Last Instagram cookies upload time (the API does not expose it).
    implementation(libs.androidx.datastore.preferences)
    // rememberLauncherForActivityResult for the SAF cookies picker.
    implementation(libs.androidx.activity.compose)
}
