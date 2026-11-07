plugins {
    alias(libs.plugins.quoker.android.application)
    alias(libs.plugins.quoker.android.compose)
    alias(libs.plugins.quoker.android.hilt)
    alias(libs.plugins.quoker.kotlin.serialization)
}

fun prop(name: String, env: String? = null): String? =
    providers.gradleProperty(name).orNull?.takeIf { it.isNotBlank() }
        ?: env?.let { providers.environmentVariable(it).orNull?.takeIf(String::isNotBlank) }

val apiBaseUrlOverride = prop("quoker.apiBaseUrl", "QUOKER_API_BASE_URL")
val signingStoreFile = prop("quoker.signing.storeFile", "QUOKER_SIGNING_STORE_FILE")

android {
    namespace = "app.quoker"

    defaultConfig {
        applicationId = "app.quoker"
        versionCode = prop("quoker.versionCode", "QUOKER_VERSION_CODE")?.toInt() ?: 1
        versionName = prop("quoker.versionName", "QUOKER_VERSION_NAME") ?: "0.1.0"
    }

    buildFeatures {
        buildConfig = true
    }

    signingConfigs {
        if (signingStoreFile != null) {
            create("release") {
                storeFile = file(signingStoreFile)
                storePassword = prop("quoker.signing.storePassword", "QUOKER_SIGNING_STORE_PASSWORD")
                keyAlias = prop("quoker.signing.keyAlias", "QUOKER_SIGNING_KEY_ALIAS")
                keyPassword = prop("quoker.signing.keyPassword", "QUOKER_SIGNING_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        debug {
            buildConfigField("String", "API_BASE_URL", "\"${apiBaseUrlOverride ?: "http://10.0.2.2:8080"}\"")
        }
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            buildConfigField("String", "API_BASE_URL", "\"${apiBaseUrlOverride ?: "https://api.quoker.ru"}\"")
            signingConfig = signingConfigs.findByName("release")
        }
    }
}

dependencies {
    implementation(project(":core:common"))
    implementation(project(":core:designsystem"))
    implementation(project(":core:network"))
    implementation(project(":core:data"))
    implementation(project(":core:download"))
    implementation(project(":core:update"))
    implementation(project(":feature:onboarding"))
    implementation(project(":feature:home"))
    implementation(project(":feature:video"))
    implementation(project(":feature:send"))
    implementation(project(":feature:history"))
    implementation(project(":feature:settings"))

    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.lifecycle.process)
    implementation(libs.androidx.navigation.compose)
    implementation(libs.androidx.hilt.navigation.compose)
    implementation(libs.androidx.hilt.lifecycle.viewmodel.compose)
    implementation(libs.androidx.work.runtime.ktx)
    implementation(libs.androidx.hilt.work)
    ksp(libs.androidx.hilt.compiler)
    implementation(libs.coil.compose)
    implementation(libs.coil.network.okhttp)
    implementation(libs.kotlinx.coroutines.android)
}
