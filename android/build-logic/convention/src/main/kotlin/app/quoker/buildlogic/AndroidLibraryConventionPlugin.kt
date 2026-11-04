package app.quoker.buildlogic

import com.android.build.api.dsl.LibraryExtension
import org.gradle.api.Plugin
import org.gradle.api.Project
import org.gradle.kotlin.dsl.configure

class AndroidLibraryConventionPlugin : Plugin<Project> {
    override fun apply(target: Project): Unit = with(target) {
        pluginManager.apply("com.android.library")

        extensions.configure<LibraryExtension> {
            compileSdk = QuokerSdk.COMPILE
            buildToolsVersion = QuokerSdk.BUILD_TOOLS
            defaultConfig {
                minSdk = QuokerSdk.MIN
                testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
                consumerProguardFiles("consumer-rules.pro")
            }
            compileOptions {
                sourceCompatibility = QuokerSdk.JAVA
                targetCompatibility = QuokerSdk.JAVA
            }
            testOptions {
                targetSdk = QuokerSdk.TARGET
                unitTests.isIncludeAndroidResources = true
            }
            lint {
                targetSdk = QuokerSdk.TARGET
            }
        }
        configureKotlinJvmTarget()
        addCommonTestDependencies()
    }
}
