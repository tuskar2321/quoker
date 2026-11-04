package app.quoker.buildlogic

import com.android.build.api.dsl.ApplicationExtension
import org.gradle.api.Plugin
import org.gradle.api.Project
import org.gradle.kotlin.dsl.configure

class AndroidApplicationConventionPlugin : Plugin<Project> {
    override fun apply(target: Project): Unit = with(target) {
        pluginManager.apply("com.android.application")

        extensions.configure<ApplicationExtension> {
            compileSdk = QuokerSdk.COMPILE
            buildToolsVersion = QuokerSdk.BUILD_TOOLS
            defaultConfig {
                minSdk = QuokerSdk.MIN
                targetSdk = QuokerSdk.TARGET
                testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
            }
            compileOptions {
                sourceCompatibility = QuokerSdk.JAVA
                targetCompatibility = QuokerSdk.JAVA
            }
            testOptions {
                unitTests.isIncludeAndroidResources = true
            }
            packaging {
                resources.excludes += setOf("/META-INF/{AL2.0,LGPL2.1}", "/META-INF/LICENSE*")
            }
        }
        configureKotlinJvmTarget()
        addCommonTestDependencies()
    }
}
