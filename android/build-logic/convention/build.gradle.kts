import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    `kotlin-dsl`
}

group = "app.quoker.buildlogic"

java {
    sourceCompatibility = JavaVersion.VERSION_17
    targetCompatibility = JavaVersion.VERSION_17
}

kotlin {
    compilerOptions {
        jvmTarget = JvmTarget.JVM_17
    }
}

// Plugins are implementation deps so modules can apply them by id without root `apply false` entries.
dependencies {
    implementation(libs.android.gradlePlugin)
    implementation(libs.kotlin.gradlePlugin)
    implementation(libs.compose.gradlePlugin)
    implementation(libs.serialization.gradlePlugin)
    implementation(libs.ksp.gradlePlugin)
    implementation(libs.hilt.gradlePlugin)
    implementation(libs.room.gradlePlugin)
}

gradlePlugin {
    plugins {
        register("androidApplication") {
            id = "quoker.android.application"
            implementationClass = "app.quoker.buildlogic.AndroidApplicationConventionPlugin"
        }
        register("androidLibrary") {
            id = "quoker.android.library"
            implementationClass = "app.quoker.buildlogic.AndroidLibraryConventionPlugin"
        }
        register("androidCompose") {
            id = "quoker.android.compose"
            implementationClass = "app.quoker.buildlogic.AndroidComposeConventionPlugin"
        }
        register("androidHilt") {
            id = "quoker.android.hilt"
            implementationClass = "app.quoker.buildlogic.AndroidHiltConventionPlugin"
        }
        register("androidRoom") {
            id = "quoker.android.room"
            implementationClass = "app.quoker.buildlogic.AndroidRoomConventionPlugin"
        }
        register("androidFeature") {
            id = "quoker.android.feature"
            implementationClass = "app.quoker.buildlogic.AndroidFeatureConventionPlugin"
        }
        register("kotlinSerialization") {
            id = "quoker.kotlin.serialization"
            implementationClass = "app.quoker.buildlogic.KotlinSerializationConventionPlugin"
        }
    }
}
