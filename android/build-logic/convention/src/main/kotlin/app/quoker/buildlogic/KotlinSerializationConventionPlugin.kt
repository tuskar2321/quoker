package app.quoker.buildlogic

import org.gradle.api.Plugin
import org.gradle.api.Project

class KotlinSerializationConventionPlugin : Plugin<Project> {
    override fun apply(target: Project): Unit = with(target) {
        pluginManager.apply("org.jetbrains.kotlin.plugin.serialization")
        dependencies.add("implementation", libs.lib("kotlinx-serialization-json"))
    }
}
