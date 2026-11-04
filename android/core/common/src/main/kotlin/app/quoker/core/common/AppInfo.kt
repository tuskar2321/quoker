package app.quoker.core.common

/** Build-time facts about the running app, provided by :app through Hilt. */
data class AppInfo(
    val apiBaseUrl: String,
    val versionCode: Int,
    val versionName: String,
)
