pluginManagement {
    includeBuild("build-logic")
    repositories {
        google {
            content {
                includeGroupByRegex("com\\.android.*")
                includeGroupByRegex("com\\.google.*")
                includeGroupByRegex("androidx.*")
            }
        }
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "quoker"

include(":app")
include(":core:common")
include(":core:designsystem")
include(":core:network")
include(":core:data")
include(":core:download")
include(":core:update")
include(":feature:onboarding")
include(":feature:home")
include(":feature:video")
include(":feature:send")
include(":feature:history")
include(":feature:settings")
